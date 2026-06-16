// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package adapters_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
	adapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
)

const (
	testOwner           = "0x1234567890123456789012345678901234567890"
	testUSDC            = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	testUSDT            = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	testWETH            = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	testWBNB            = "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"
	testRouter          = "0x1111111111111111111111111111111111111111"
	testApprove         = "0x2222222222222222222222222222222222222222"
	testCallData        = "0xdeadbeef"
	testBaseUnitsAmount = "1000000"
	testHumanValue      = "1"

	wantZeroAddress        = "0x0000000000000000000000000000000000000000"
	wantEIP155NativeToken  = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"
	wantUniswapRouterEth   = "0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"
	wantPancakeV2RouterBSC = "0x10ED43C718714eb63d5aA57B78B54704E256024E"
	wantAddressThis        = "0x0000000000000000000000000000000000000002"
)

// staticDecimalsSource implements swap.DecimalsSource returning a fixed value for all tokens.
type staticDecimalsSource struct{ d uint8 }

func (s staticDecimalsSource) Decimals(_ context.Context, _, _ string) (uint8, error) {
	return s.d, nil
}

func testDecimalsResolver(d uint8) *swap.DecimalsResolver {
	src := staticDecimalsSource{d}
	return swap.NewDecimalsResolver(src, nil)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func decodeBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
}

func quoteReq(protocol string) swap.QuoteRequest {
	return swap.QuoteRequest{
		Chain:         "ethereum",
		FromAddress:   testOwner,
		SourceAsset:   testUSDC,
		TargetAsset:   testUSDT,
		Amount:        testBaseUnitsAmount,
		DisplayAmount: testHumanValue,
		Slippage:      0.5,
	}
}

func TestOdosQuote_NormalizesNativeToZeroAddressAndRequiresPathID(t *testing.T) {
	var captured map[string]interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/sor/quote/v2" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		captured = decodeBody(t, r)
		return jsonResponse(http.StatusOK, `{"pathId":"path-1","outAmounts":["232421"],"gasEstimate":12345}`), nil
	})}

	adapter := adapters.NewOdosAdapter()
	adapters.ConfigureHTTPForTest(adapter, client, "https://odos.test")
	req := quoteReq("odos")
	req.SourceAsset = "native"
	req.TargetAsset = "0x0"

	result, err := adapter.Quote(context.Background(), req)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if result.OutputAmount != "232421" {
		t.Fatalf("output = %s, want 232421", result.OutputAmount)
	}
	inputs := captured["inputTokens"].([]interface{})
	outputs := captured["outputTokens"].([]interface{})
	if got := inputs[0].(map[string]interface{})["tokenAddress"]; got != wantZeroAddress {
		t.Fatalf("source native token = %v, want %s", got, wantZeroAddress)
	}
	if got := outputs[0].(map[string]interface{})["tokenAddress"]; got != wantZeroAddress {
		t.Fatalf("target native token = %v, want %s", got, wantZeroAddress)
	}
	if got := captured["referralCode"]; got == nil {
		t.Fatal("expected Odos referralCode to be sent")
	}
}

func TestOdosQuote_MissingPathIDFails(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"outAmounts":["232421"]}`), nil
	})}

	adapter := adapters.NewOdosAdapter()
	adapters.ConfigureHTTPForTest(adapter, client, "https://odos.test")
	_, err := adapter.Quote(context.Background(), quoteReq("odos"))
	if err == nil || !strings.Contains(err.Error(), "pathId") {
		t.Fatalf("expected missing pathId error, got %v", err)
	}
}

func TestKyberBuild_UsesChainScopedRouteBuild(t *testing.T) {
	var captured map[string]interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/ethereum/api/v1/route/build" {
			t.Fatalf("path = %s, want /ethereum/api/v1/route/build", r.URL.Path)
		}
		captured = decodeBody(t, r)
		return jsonResponse(http.StatusOK, `{"code":0,"data":{"routerAddress":"`+testRouter+`","data":"`+testCallData+`","gas":"210000"}}`), nil
	})}

	adapter := adapters.NewKyberAdapter()
	adapters.ConfigureHTTPForTest(adapter, client, "https://kyber.test")
	result, err := adapter.Build(context.Background(), swap.BuildRequest{
		Chain:       "ethereum",
		FromAddress: testOwner,
		Quote: map[string]interface{}{
			"route_summary": map[string]interface{}{"amountOut": "232421"},
			"chain_slug":    "ethereum",
			"is_native_in":  false,
			"amount_in":     "1000000",
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.To != testRouter || result.Data != testCallData {
		t.Fatalf("unexpected build result: %+v", result)
	}
	if captured["sender"] != testOwner || captured["recipient"] != testOwner {
		t.Fatalf("sender/recipient not propagated: %v", captured)
	}
}

func TestOpenOceanQuote_UsesHumanDisplayAmountAndBuildsExecutableTx(t *testing.T) {
	var sawQuote bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v3/eth/quote":
			sawQuote = true
			if r.URL.Query().Get("amount") != testHumanValue {
				t.Fatalf("amount = %s, want %s", r.URL.Query().Get("amount"), testHumanValue)
			}
			return jsonResponse(http.StatusOK, `{"code":200,"data":{"outAmount":"232421"}}`), nil
		case "/v3/eth/swap_quote":
			if r.URL.Query().Get("account") != testOwner {
				t.Fatalf("account = %s, want %s", r.URL.Query().Get("account"), testOwner)
			}
			return jsonResponse(http.StatusOK, `{"code":200,"data":{"to":"`+testRouter+`","data":"`+testCallData+`","value":"0","gas":"210000"}}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(http.StatusNotFound, `{}`), nil
	})}

	adapter := adapters.NewOpenOceanAdapter()
	adapters.ConfigureHTTPForTest(adapter, client, "https://openocean.test")
	quote, err := adapter.Quote(context.Background(), quoteReq("openocean"))
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if !sawQuote || quote.OutputAmount != "232421" {
		t.Fatalf("quote not parsed: %+v", quote)
	}
	build, err := adapter.Build(context.Background(), swap.BuildRequest{Quote: quote.RawQuote})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if build.To != testRouter || build.Data != testCallData {
		t.Fatalf("unexpected build result: %+v", build)
	}

	// Verify RawQuote is flat and does not contain the _openocean_internal nested key
	rawMap, _ := quote.RawQuote.(map[string]interface{})
	if _, hasNested := rawMap["_openocean_internal"]; hasNested {
		t.Fatal("RawQuote should be flat, not contain _openocean_internal")
	}
	if rawMap["chain_code"] == nil {
		t.Fatal("flat RawQuote should contain chain_code")
	}
}

func TestParaSwapQuote_UsesTokenTransferProxyAsApprovalTarget(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prices" {
			t.Fatalf("path = %s, want /prices", r.URL.Path)
		}
		if r.URL.Query().Get("network") != "1" {
			t.Fatalf("network = %s, want 1", r.URL.Query().Get("network"))
		}
		return jsonResponse(http.StatusOK, `{"priceRoute":{"destAmount":"232421","tokenTransferProxy":"`+testApprove+`","contractAddress":"`+testRouter+`"}}`), nil
	})}

	adapter := adapters.NewParaSwapAdapter(testDecimalsResolver(6))
	adapters.ConfigureHTTPForTest(adapter, client, "https://paraswap.test")
	result, err := adapter.Quote(context.Background(), quoteReq("paraswap"))
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if result.ApproveTarget != testApprove {
		t.Fatalf("approve target = %s, want %s", result.ApproveTarget, testApprove)
	}
}

func TestUniswapBuild_UniversalRouterNativeOutputUsesRouterRecipient(t *testing.T) {
	adapter := adapters.NewUniswapAdapter(nil)
	build, err := adapter.Build(context.Background(), swap.BuildRequest{
		Quote: map[string]interface{}{
			"amount_in":      testBaseUnitsAmount,
			"amount_out_min": "990000",
			"deadline":       float64(9999999999),
			"is_native_in":   false,
			"is_native_out":  true,
			"token_in":       testUSDC,
			"token_out":      testWETH,
			"swap_router":    wantUniswapRouterEth,
			"weth":           testWETH,
			"version":        "v3",
			"fee":            float64(3000),
			"tick_spacing":   float64(0),
			"currency0":      "",
			"currency1":      "",
			"zero_for_one":   false,
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if build.To != wantUniswapRouterEth {
		t.Fatalf("to = %s, want universal router", build.To)
	}
	if build.Value != "0x0" {
		t.Fatalf("value = %s, want 0x0", build.Value)
	}
	if !strings.HasPrefix(build.Data, "0x3593564c") {
		t.Fatalf("data selector = %s, want execute selector", build.Data[:10])
	}
	if !strings.Contains(strings.ToLower(build.Data), strings.TrimPrefix(wantAddressThis, "0x")) {
		t.Fatal("native-output swap must send WETH to ADDRESS_THIS for unwrap")
	}
}

func TestPancakeBuild_V2NativeInputUsesRouterMethod(t *testing.T) {
	adapter := adapters.NewPancakeAdapter(nil)
	build, err := adapter.Build(context.Background(), swap.BuildRequest{
		Quote: map[string]interface{}{
			"chain_id":       float64(56),
			"amount_in":      "100000000000000",
			"amount_out_min": "990000",
			"deadline":       float64(9999999999),
			"is_native_in":   true,
			"is_native_out":  false,
			"token_in":       testWBNB,
			"token_out":      testUSDT,
			"sender":         testOwner,
			"swap_router":    wantPancakeV2RouterBSC,
			"weth":           testWBNB,
			"version":        "pancake_v2",
			"fee":            float64(0),
			"tick_spacing":   float64(0),
			"currency0":      "",
			"currency1":      "",
			"zero_for_one":   false,
			"pool_manager":   "",
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if build.To != wantPancakeV2RouterBSC {
		t.Fatalf("to = %s, want Pancake V2 router", build.To)
	}
	if build.Value != "0x5af3107a4000" {
		t.Fatalf("value = %s, want input amount as hex", build.Value)
	}
	if !strings.HasPrefix(build.Data, "0x7ff36ab5") {
		t.Fatalf("data = %s, want swapExactETHForTokens selector", build.Data[:10])
	}
}

func TestOdosBuild_SendsPathIDToAssemble(t *testing.T) {
	var captured map[string]interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/sor/assemble" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		captured = decodeBody(t, r)
		return jsonResponse(http.StatusOK, `{"transaction":{"to":"`+testRouter+`","value":"0","data":"`+testCallData+`","gas":210000}}`), nil
	})}

	adapter := adapters.NewOdosAdapter()
	adapters.ConfigureHTTPForTest(adapter, client, "https://odos.test")
	result, err := adapter.Build(context.Background(), swap.BuildRequest{
		FromAddress: testOwner,
		Quote: map[string]interface{}{
			"path_id": "flat-path-1",
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.To != testRouter {
		t.Fatalf("to = %s, want %s", result.To, testRouter)
	}
	if captured["pathId"] != "flat-path-1" {
		t.Fatalf("pathId in request = %v, want flat-path-1", captured["pathId"])
	}
}

func TestParaSwapBuild_SendsCorrectRequestBody(t *testing.T) {
	var captured map[string]interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		// path: /transactions/1?ignoreChecks=true
		if !strings.HasPrefix(r.URL.Path, "/transactions/1") {
			t.Fatalf("path = %s, want /transactions/1*", r.URL.Path)
		}
		captured = decodeBody(t, r)
		return jsonResponse(http.StatusOK, `{"to":"`+testRouter+`","data":"`+testCallData+`","value":"0","gas":"210000"}`), nil
	})}

	adapter := adapters.NewParaSwapAdapter(testDecimalsResolver(6))
	adapters.ConfigureHTTPForTest(adapter, client, "https://paraswap.test")

	priceRoute := json.RawMessage(`{"destAmount":"232421","contractAddress":"` + testRouter + `"}`)
	result, err := adapter.Build(context.Background(), swap.BuildRequest{
		FromAddress: testOwner,
		SlippageBPS: 50,
		Quote: map[string]interface{}{
			"price_route": priceRoute,
			"network":     float64(1),
			"src_token":   testUSDC,
			"dest_token":  testUSDT,
			"src_amount":  testBaseUnitsAmount,
			"swapper":     testOwner,
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.To != testRouter {
		t.Fatalf("to = %s, want %s", result.To, testRouter)
	}
	if captured["srcToken"] != testUSDC {
		t.Fatalf("srcToken = %v, want %s", captured["srcToken"], testUSDC)
	}
	if captured["userAddress"] != testOwner {
		t.Fatalf("userAddress = %v, want %s", captured["userAddress"], testOwner)
	}
}
