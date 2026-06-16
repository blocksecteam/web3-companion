// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VersionInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	UpdateURL string `json:"update_url,omitempty"`
	HasUpdate bool   `json:"has_update"`
	CheckedAt string `json:"checked_at,omitempty"`
}

type VersionChecker struct {
	current string
	image   string
	mu      sync.RWMutex
	cached  VersionInfo
	client  *http.Client
}

const (
	versionCheckInterval = 6 * time.Hour
	versionCheckTimeout  = 10 * time.Second
)

func NewVersionChecker(current, dockerImage string) *VersionChecker {
	return &VersionChecker{
		current: current,
		image:   dockerImage,
		cached:  VersionInfo{Current: current},
		client:  &http.Client{Timeout: versionCheckTimeout},
	}
}

func (vc *VersionChecker) Start() {
	go func() {
		vc.check()
		ticker := time.NewTicker(versionCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			vc.check()
		}
	}()
}

func (vc *VersionChecker) Info() VersionInfo {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.cached
}

func (vc *VersionChecker) check() {
	latest, err := vc.fetchLatestTag()
	if err != nil || latest == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	info := VersionInfo{
		Current:   vc.current,
		Latest:    latest,
		HasUpdate: IsNewerVersion(vc.current, latest),
		CheckedAt: now,
	}
	if info.HasUpdate {
		info.UpdateURL = fmt.Sprintf("https://hub.docker.com/r/%s/tags?name=%s", vc.image, latest)
	}
	vc.mu.Lock()
	vc.cached = info
	vc.mu.Unlock()
}

func (vc *VersionChecker) fetchLatestTag() (string, error) {
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=50&ordering=last_updated", vc.image)
	resp, err := vc.client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker hub API status %d", resp.StatusCode)
	}
	var result struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	var best []int
	var bestTag string
	for _, tag := range result.Results {
		v := parseSemver(tag.Name)
		if v == nil {
			continue
		}
		if best == nil || v[0] > best[0] || (v[0] == best[0] && v[1] > best[1]) || (v[0] == best[0] && v[1] == best[1] && v[2] > best[2]) {
			best = v
			bestTag = tag.Name
		}
	}
	return bestTag, nil
}

func IsNewerVersion(current, latest string) bool {
	cur := parseSemver(current)
	lat := parseSemver(latest)
	if cur == nil || lat == nil {
		return false
	}
	if lat[0] != cur[0] {
		return lat[0] > cur[0]
	}
	if lat[1] != cur[1] {
		return lat[1] > cur[1]
	}
	return lat[2] > cur[2]
}

func parseSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
