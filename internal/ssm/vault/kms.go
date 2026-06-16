// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/blocksecteam/web3-companion/internal/config"
)

type KMSClient struct {
	client   *kms.Client
	keyID    string
	enabled  bool
	localKEK []byte
}

func NewKMSClient(ctx context.Context, region, keyID string) (*KMSClient, error) {
	cfg := config.LoadFromEnv()
	return NewKMSClientWithCredentials(ctx, region, keyID, cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey)
}

func NewKMSClientWithCredentials(ctx context.Context, region, keyID, accessKeyID, secretAccessKey string) (*KMSClient, error) {
	if keyID == "" || region == "" || accessKeyID == "" || secretAccessKey == "" {
		return nil, fmt.Errorf("KMS is required: set AWS_REGION, AWS_KMS_KEY_ID, AWS_ACCESS_KEY_ID, and AWS_SECRET_ACCESS_KEY")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("kms: load config: %w", err)
	}
	client := kms.NewFromConfig(cfg)
	if _, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(keyID)}); err != nil {
		return nil, fmt.Errorf("kms: describe key: %w", err)
	}
	return &KMSClient{
		client:  client,
		keyID:   keyID,
		enabled: true,
	}, nil
}

func NewTestKMSClient() *KMSClient {
	return &KMSClient{enabled: false}
}

func NewEncryptedFileKMSClient(rootKey string) (*KMSClient, error) {
	rootKey = strings.TrimSpace(rootKey)
	if len([]byte(rootKey)) < 32 {
		return nil, fmt.Errorf("WEB3_SSM_KEY must be at least 32 bytes")
	}
	sum := sha256.Sum256([]byte("web3-companion:ssm:v1:" + rootKey))
	kek := make([]byte, len(sum))
	copy(kek, sum[:])
	return &KMSClient{localKEK: kek}, nil
}

func (k *KMSClient) IsEnabled() bool {
	return k.enabled
}

func (k *KMSClient) IsEncryptedFile() bool {
	return !k.enabled && len(k.localKEK) == 32
}

func (k *KMSClient) GenerateDataKey(ctx context.Context) (plaintext, encrypted []byte, err error) {
	if !k.enabled {
		pt, err := GenerateDataKey()
		if err != nil {
			return nil, nil, err
		}
		if len(k.localKEK) == 32 {
			encrypted, err := Encrypt(k.localKEK, pt)
			if err != nil {
				ZeroBytes(pt)
				return nil, nil, err
			}
			return pt, encrypted, nil
		}

		return pt, append([]byte(nil), pt...), nil
	}
	out, err := k.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   aws.String(k.keyID),
		KeySpec: types.DataKeySpecAes256,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("kms: generate data key: %w", err)
	}
	return out.Plaintext, out.CiphertextBlob, nil
}

func (k *KMSClient) Decrypt(ctx context.Context, ciphertextBlob []byte) ([]byte, error) {
	if !k.enabled {
		if len(k.localKEK) == 32 {
			return Decrypt(k.localKEK, ciphertextBlob)
		}

		return append([]byte(nil), ciphertextBlob...), nil
	}
	out, err := k.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId:               aws.String(k.keyID),
		CiphertextBlob:      ciphertextBlob,
		EncryptionAlgorithm: types.EncryptionAlgorithmSpecSymmetricDefault,
	})
	if err != nil {
		return nil, fmt.Errorf("kms: decrypt: %w", err)
	}
	return out.Plaintext, nil
}
