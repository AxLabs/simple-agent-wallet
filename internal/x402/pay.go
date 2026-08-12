package x402pay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	x402http "github.com/x402-foundation/x402/go/v2/http"
	evmmech "github.com/x402-foundation/x402/go/v2/mechanisms/evm"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
	"github.com/x402-foundation/x402/go/v2/types"
)

// AcceptView is a secret-free inspect row.
type AcceptView struct {
	Index               int                    `json:"index"`
	Scheme              string                 `json:"scheme"`
	Network             string                 `json:"network"`
	Family              string                 `json:"family"`
	Asset               string                 `json:"asset"`
	Amount              string                 `json:"amount"`
	PayTo               string                 `json:"payTo"`
	MaxTimeoutSeconds   int                    `json:"maxTimeoutSeconds,omitempty"`
	AssetTransferMethod string                 `json:"assetTransferMethod,omitempty"`
	FeePayer            string                 `json:"feePayer,omitempty"`
	Extra               map[string]interface{} `json:"extra,omitempty"`
}

type InspectResult struct {
	URL         string       `json:"url"`
	Status      int          `json:"status"`
	X402Version int          `json:"x402Version"`
	Error       string       `json:"error,omitempty"`
	Accepts     []AcceptView `json:"accepts"`
}

type SelectOpts struct {
	Network string
	Asset   string
	Method  string // eip3009|permit2 (exact)
	Scheme  string // exact|batch-settlement
	// Index selects accepts[i] when PreferIndex is true.
	Index       int
	PreferIndex bool
}

type PayResult struct {
	OK              bool                   `json:"ok"`
	Status          int                    `json:"status"`
	Selected        AcceptView             `json:"selected"`
	PaymentResponse map[string]interface{} `json:"paymentResponse,omitempty"`
	BodyPreview     string                 `json:"bodyPreview,omitempty"`
	Headers         map[string]string      `json:"headers,omitempty"`
}

func FamilyOf(network string) string {
	switch {
	case strings.HasPrefix(network, "eip155:"):
		return store.FamilyEVM
	case strings.HasPrefix(network, "solana"):
		return store.FamilySolana
	case strings.HasPrefix(network, "hedera:"):
		return store.FamilyHedera
	default:
		switch network {
		case "base", "base-sepolia", "ethereum", "sepolia", "polygon", "abstract", "abstract-testnet", "peak":
			return store.FamilyEVM
		case "solana", "solana-devnet":
			return store.FamilySolana
		default:
			return "unknown"
		}
	}
}

func ViewFromV2(i int, r types.PaymentRequirements) AcceptView {
	v := AcceptView{
		Index:             i,
		Scheme:            r.Scheme,
		Network:           r.Network,
		Family:            FamilyOf(r.Network),
		Asset:             r.Asset,
		Amount:            r.Amount,
		PayTo:             r.PayTo,
		MaxTimeoutSeconds: r.MaxTimeoutSeconds,
		Extra:             r.Extra,
	}
	if r.Extra != nil {
		if m, ok := r.Extra["assetTransferMethod"].(string); ok {
			v.AssetTransferMethod = m
		}
		if f, ok := r.Extra["feePayer"].(string); ok {
			v.FeePayer = f
		}
	}
	if v.AssetTransferMethod == "" && v.Family == store.FamilyEVM && r.Scheme == "exact" {
		v.AssetTransferMethod = string(evmmech.AssetTransferMethodEIP3009)
	}
	return v
}

func ViewFromV1(i int, r types.PaymentRequirementsV1) AcceptView {
	extra := r.GetExtra()
	v := AcceptView{
		Index:             i,
		Scheme:            r.Scheme,
		Network:           r.Network,
		Family:            FamilyOf(r.Network),
		Asset:             r.Asset,
		Amount:            r.MaxAmountRequired,
		PayTo:             r.PayTo,
		MaxTimeoutSeconds: r.MaxTimeoutSeconds,
		Extra:             extra,
	}
	if m, ok := extra["assetTransferMethod"].(string); ok {
		v.AssetTransferMethod = m
	}
	if f, ok := extra["feePayer"].(string); ok {
		v.FeePayer = f
	}
	if v.AssetTransferMethod == "" && v.Family == store.FamilyEVM && r.Scheme == "exact" {
		v.AssetTransferMethod = string(evmmech.AssetTransferMethodEIP3009)
	}
	return v
}

// Fetch402 performs the request and returns inspect data (works for 200 or 402).
func Fetch402(ctx context.Context, method, url string, body []byte, headers map[string]string) (*InspectResult, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		if req.Header.Get("Content-Type") == "" && len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, nil, err
	}
	out := &InspectResult{URL: url, Status: resp.StatusCode}
	if resp.StatusCode != http.StatusPaymentRequired {
		return out, respBody, resp.Header, nil
	}
	hdrMap := headerMap(resp.Header)
	if err := fillInspect(out, hdrMap, respBody); err != nil {
		return nil, respBody, resp.Header, err
	}
	return out, respBody, resp.Header, nil
}

func fillInspect(out *InspectResult, headers map[string]string, body []byte) error {
	normalized := make(map[string]string, len(headers))
	for k, v := range headers {
		normalized[strings.ToUpper(k)] = v
	}
	if h, ok := normalized["PAYMENT-REQUIRED"]; ok {
		pr, err := DecodePaymentRequiredHeader(h)
		if err != nil {
			return err
		}
		out.X402Version = pr.X402Version
		out.Error = pr.Error
		for i, a := range pr.Accepts {
			out.Accepts = append(out.Accepts, ViewFromV2(i, a))
		}
		return nil
	}
	v1, err := types.ToPaymentRequiredV1(body)
	if err == nil && v1.X402Version == 1 {
		out.X402Version = 1
		out.Error = v1.Error
		for i, a := range v1.Accepts {
			out.Accepts = append(out.Accepts, ViewFromV1(i, a))
		}
		return nil
	}
	pr, err := types.ToPaymentRequired(body)
	if err != nil {
		return fmt.Errorf("no payment required information found")
	}
	out.X402Version = pr.X402Version
	out.Error = pr.Error
	for i, a := range pr.Accepts {
		out.Accepts = append(out.Accepts, ViewFromV2(i, a))
	}
	return nil
}

func headerMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			m[k] = vs[0]
		}
	}
	return m
}

func SelectAccept(accepts []AcceptView, opts SelectOpts) (AcceptView, error) {
	if len(accepts) == 0 {
		return AcceptView{}, fmt.Errorf("no accepts")
	}
	if opts.PreferIndex {
		if opts.Index < 0 || opts.Index >= len(accepts) {
			return AcceptView{}, fmt.Errorf("accept index %d out of range", opts.Index)
		}
		return accepts[opts.Index], nil
	}
	var filtered []AcceptView
	for _, a := range accepts {
		if opts.Network != "" && !networkMatch(a.Network, opts.Network) {
			continue
		}
		if opts.Asset != "" && !strings.EqualFold(stripAssetPrefix(a.Asset), stripAssetPrefix(opts.Asset)) {
			continue
		}
		if opts.Method != "" && a.Family == store.FamilyEVM {
			m := a.AssetTransferMethod
			if m == "" {
				m = string(evmmech.AssetTransferMethodEIP3009)
			}
			if !strings.EqualFold(m, opts.Method) {
				continue
			}
		}
		if opts.Scheme != "" && !strings.EqualFold(a.Scheme, opts.Scheme) {
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 {
		return AcceptView{}, fmt.Errorf("no accept matched filters (network=%q asset=%q method=%q scheme=%q)", opts.Network, opts.Asset, opts.Method, opts.Scheme)
	}
	return filtered[0], nil
}

func networkMatch(have, want string) bool {
	if strings.EqualFold(have, want) {
		return true
	}
	if strings.HasPrefix(have, "eip155:") && have[len("eip155:"):] == want {
		return true
	}
	if strings.HasPrefix(want, "eip155:") && want[len("eip155:"):] == have {
		return true
	}
	return false
}

func stripAssetPrefix(a string) string {
	if i := strings.LastIndex(a, ":"); i >= 0 && (strings.HasPrefix(a, "erc20:") || strings.HasPrefix(a, "spl:")) {
		return a[i+1:]
	}
	return a
}

// Pay performs one 402→sign→retry cycle (exact or batch-settlement).
func Pay(ctx context.Context, w *store.Wallet, cfg *config.Config, method, url string, body []byte, headers map[string]string, opts SelectOpts) (*PayResult, error) {
	insp, respBody, respHdr, err := Fetch402(ctx, method, url, body, headers)
	if err != nil {
		return nil, err
	}
	if insp.Status != http.StatusPaymentRequired {
		return &PayResult{
			OK:          insp.Status >= 200 && insp.Status < 300,
			Status:      insp.Status,
			BodyPreview: truncate(string(respBody), 512),
		}, nil
	}
	selected, err := SelectAccept(insp.Accepts, opts)
	if err != nil {
		return nil, err
	}
	if !w.HasFamily(selected.Family) {
		return nil, fmt.Errorf("selected accept family %q but wallet has no %s credentials", selected.Family, selected.Family)
	}

	built, err := BuildClient(w, cfg, selected.Network)
	if err != nil {
		return nil, err
	}
	client := built.Client
	hc := x402http.NewClient(client)

	var payloadBytes []byte
	if insp.X402Version == 1 {
		v1, err := types.ToPaymentRequiredV1(respBody)
		if err != nil {
			return nil, err
		}
		req := v1.Accepts[selected.Index]
		if !opts.PreferIndex {
			req, err = matchTypedV1(v1.Accepts, selected)
			if err != nil {
				return nil, err
			}
		}
		payload, err := client.CreatePaymentPayloadV1(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("create v1 payload: %w", err)
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	} else {
		hdrMap := headerMap(respHdr)
		pr, err := hc.GetPaymentRequiredResponse(hdrMap, respBody)
		if err != nil {
			return nil, err
		}
		req := pr.Accepts[selected.Index]
		if !opts.PreferIndex {
			req, err = matchTyped(pr.Accepts, selected)
			if err != nil {
				return nil, err
			}
		}
		var resource *types.ResourceInfo
		if pr.Resource != nil {
			resource = pr.Resource
		}
		payload, err := client.CreatePaymentPayload(ctx, req, resource, pr.Extensions)
		if err != nil {
			return nil, fmt.Errorf("create payload: %w", err)
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	payHdrs, err := hc.EncodePaymentSignatureHeader(payloadBytes)
	if err != nil {
		return nil, err
	}
	retryHeaders := map[string]string{}
	for k, v := range headers {
		retryHeaders[k] = v
	}
	for k, v := range payHdrs {
		retryHeaders[k] = v
	}

	req2, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range retryHeaders {
		req2.Header.Set(k, v)
	}
	if (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) && req2.Header.Get("Content-Type") == "" && len(body) > 0 {
		req2.Header.Set("Content-Type", "application/json")
	}
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)

	result := &PayResult{
		OK:       resp2.StatusCode >= 200 && resp2.StatusCode < 300,
		Status:   resp2.StatusCode,
		Selected: selected,
		Headers:  payHdrs,
	}
	if settle, err := hc.GetPaymentSettleResponse(headerMap(resp2.Header)); err == nil && settle != nil {
		b, _ := json.Marshal(settle)
		_ = json.Unmarshal(b, &result.PaymentResponse)
	}
	result.BodyPreview = truncate(string(body2), 1024)
	return result, nil
}

func matchTyped(accepts []types.PaymentRequirements, selected AcceptView) (types.PaymentRequirements, error) {
	for _, a := range accepts {
		if a.Scheme == selected.Scheme && a.Network == selected.Network && a.Asset == selected.Asset && a.PayTo == selected.PayTo && a.Amount == selected.Amount {
			return a, nil
		}
	}
	if selected.Index >= 0 && selected.Index < len(accepts) {
		return accepts[selected.Index], nil
	}
	return types.PaymentRequirements{}, fmt.Errorf("could not rematch selected accept")
}

func matchTypedV1(accepts []types.PaymentRequirementsV1, selected AcceptView) (types.PaymentRequirementsV1, error) {
	for _, a := range accepts {
		if a.Scheme == selected.Scheme && a.Network == selected.Network && a.Asset == selected.Asset && a.PayTo == selected.PayTo && a.MaxAmountRequired == selected.Amount {
			return a, nil
		}
	}
	if selected.Index >= 0 && selected.Index < len(accepts) {
		return accepts[selected.Index], nil
	}
	return types.PaymentRequirementsV1{}, fmt.Errorf("could not rematch selected v1 accept")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// PollOrderStatus replaces {tx} in URL and polls until timeout or success-looking JSON.
func PollOrderStatus(ctx context.Context, urlTemplate, txHash string, interval, timeout time.Duration) (map[string]interface{}, error) {
	u := strings.ReplaceAll(urlTemplate, "{tx}", txHash)
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if st, ok := m["status"].(string); ok {
				if st == "success" || st == "paid" || st == "settled" || st == "complete" {
					return m, nil
				}
			} else {
				return m, nil
			}
		}
		if time.Now().After(deadline) {
			return m, fmt.Errorf("order-status poll timeout")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// DecodePaymentRequiredHeader is exported for tests.
func DecodePaymentRequiredHeader(b64 string) (*types.PaymentRequired, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return types.ToPaymentRequired(raw)
}

func ChainIDFromNetwork(network string) (int64, error) {
	if strings.HasPrefix(network, "eip155:") {
		return strconv.ParseInt(strings.TrimPrefix(network, "eip155:"), 10, 64)
	}
	return 0, fmt.Errorf("not an eip155 network: %s", network)
}

func hederaCAIP2(net string) string {
	switch strings.ToLower(strings.TrimSpace(net)) {
	case "", "testnet", hederamech.HederaTestnetCAIP2:
		return hederamech.HederaTestnetCAIP2
	case "mainnet", hederamech.HederaMainnetCAIP2:
		return hederamech.HederaMainnetCAIP2
	default:
		if strings.HasPrefix(net, "hedera:") {
			return net
		}
		return hederamech.HederaTestnetCAIP2
	}
}
