package devops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// APIVersion is sent with every request unless the caller sets its own.
	APIVersion = "7.1"

	// adoResource is the well-known Entra application ID of Azure DevOps. A
	// token issued for it works against every dev.azure.com endpoint.
	adoResource = "499b84ac-1321-427f-aa17-267ca6975798"

	maxAttempts = 4
	maxBody     = 64 << 20
)

// Client calls the Azure DevOps REST API directly.
//
// Most of dvo shells out to `az`, but a single run of `workitem activity`
// needs hundreds of small calls. `az rest` starts a Python interpreter for each
// one, so the client takes one token from `az account get-access-token` and
// talks HTTP itself. It is safe for concurrent use.
type Client struct {
	// BaseURL is https://dev.azure.com/<org> without a trailing slash.
	BaseURL string
	HTTP    *http.Client

	// TokenSource returns a bearer token. NewClient wires it to the Azure CLI;
	// tests replace it.
	TokenSource func(ctx context.Context) (string, error)

	// Tenant is the Entra tenant to request the token for. Empty means: ask
	// the organization (see discoverTenant).
	Tenant string

	// TenantIsHint marks Tenant as remembered from an earlier run rather than
	// demanded by the user. A hint that the organization rejects is dropped and
	// detected again; an explicit tenant is never second-guessed.
	TenantIsHint bool

	// OnTenant is called with a tenant that was detected (not supplied), so the
	// caller can remember it.
	OnTenant func(tenant string)

	// PAT is the fallback for when the Entra token is rejected, e.g. because
	// the account is not usable in the organization's tenant. It is sent with
	// Basic auth. PATs are being phased out, so the token comes first.
	PAT    string
	usePAT atomic.Bool

	mu      sync.Mutex
	token   string
	backoff time.Duration
}

// NewClient returns a client for the organization at orgURL.
func NewClient(orgURL string) *Client {
	c := &Client{
		BaseURL: strings.TrimRight(orgURL, "/"),
		HTTP:    &http.Client{Timeout: 90 * time.Second},
		backoff: time.Second,
	}
	c.TokenSource = c.azToken
	return c
}

// azToken fetches a token from the Azure CLI for the organization's tenant.
//
// az hands out tokens for its default tenant, but an organization only accepts
// tokens from the tenant it is connected to. Asking for the wrong one is
// answered with TF400813 on every call except connectionData.
func (c *Client) azToken(ctx context.Context) (string, error) {
	if c.Tenant == "" {
		if c.Tenant = c.discoverTenant(ctx); c.Tenant != "" {
			c.remember()
		}
	}
	if c.Tenant != "" {
		return azAccessToken(ctx, c.Tenant)
	}
	return c.probeTenants(ctx)
}

func (c *Client) remember() {
	if c.OnTenant != nil {
		c.OnTenant(c.Tenant)
	}
}

// probeTenants is the last resort for organizations that are not connected to
// an Entra directory (they announce the all-zero tenant). Accounts can still be
// guests in some tenant, so each tenant az is logged into is tried with a cheap
// authenticated request and the first one the organization accepts is kept.
// When none is accepted the default tenant's token is returned, so the caller
// gets the service's own error.
func (c *Client) probeTenants(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "az", "account", "list",
		"--query", "[].tenantId", "--output", "tsv").Output()
	if err == nil {
		seen := map[string]bool{}
		for _, t := range strings.Fields(string(out)) {
			if seen[t] {
				continue
			}
			seen[t] = true
			tok, err := azAccessToken(ctx, t)
			if err == nil && c.accepts(ctx, tok) {
				c.Tenant = t
				c.remember()
				return tok, nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
	}
	return azAccessToken(ctx, "")
}

// accepts reports whether the organization authenticates token. connectionData
// is no use for this: it answers 200 to any valid token.
func (c *Client) accepts(ctx context.Context, token string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/_apis/projects?$top=1&api-version="+APIVersion, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	noRedirect := *c.HTTP
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// discoverTenant reads the tenant ID that Azure DevOps announces in the
// X-VSS-ResourceTenant header, even on an unauthenticated request. It returns
// "" when that fails, which makes az fall back to its default tenant.
func (c *Client) discoverTenant(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/_apis/connectionData", nil)
	if err != nil {
		return ""
	}
	noRedirect := *c.HTTP
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	id := strings.TrimSpace(resp.Header.Get("X-VSS-ResourceTenant"))
	// The header can list several tenants; the zero GUID means "none".
	id, _, _ = strings.Cut(id, ",")
	if id == "00000000-0000-0000-0000-000000000000" {
		return ""
	}
	return id
}

// azAccessToken asks the Azure CLI for an Azure DevOps access token.
func azAccessToken(ctx context.Context, tenant string) (string, error) {
	args := []string{"account", "get-access-token",
		"--resource", adoResource,
		"--query", "accessToken",
		"--output", "tsv",
	}
	if tenant != "" {
		args = append(args, "--tenant", tenant)
	}
	out, err := exec.CommandContext(ctx, "az", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("az account get-access-token failed (try `az login --tenant %s`): %s",
				tenant, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("az account get-access-token failed: %w", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("az account get-access-token returned no token")
	}
	return tok, nil
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	tok, err := c.TokenSource(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	return tok, nil
}

// invalidate drops used, unless another goroutine already replaced it.
func (c *Client) invalidate(used string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == used {
		c.token = ""
		if c.TenantIsHint {
			c.Tenant, c.TenantIsHint = "", false
		}
	}
}

// Get requests path (relative to BaseURL, e.g. "/_apis/wit/wiql") and decodes
// the JSON response into dst. dst may be nil.
func (c *Client) Get(ctx context.Context, path string, query url.Values, dst any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, dst)
}

// Post sends body as JSON and decodes the JSON response into dst.
func (c *Client) Post(ctx context.Context, path string, body, dst any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, nil, payload, dst)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, dst any) error {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	if q.Get("api-version") == "" {
		q.Set("api-version", APIVersion)
	}
	target := c.BaseURL + path + "?" + q.Encode()

	refreshed := false
	for attempt := 0; ; attempt++ {
		auth, tok, err := c.authorization(ctx)
		if err != nil {
			return err
		}

		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rdr)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		switch {
		// 203 is what Azure DevOps answers when it did not accept the
		// credentials; treat it like a 401 and retry once with a fresh token.
		case rejected(resp) && !refreshed && !c.usePAT.Load():
			refreshed = true
			c.invalidate(tok)
			attempt--
			continue
		// Still rejected with a fresh token: switch every request to the PAT.
		case rejected(resp) && c.PAT != "" && !c.usePAT.Load():
			c.usePAT.Store(true)
			attempt--
			continue
		case (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 502 && resp.StatusCode <= 504) && attempt < maxAttempts-1:
			if err := sleepCtx(ctx, c.retryDelay(resp, attempt)); err != nil {
				return err
			}
			continue
		case resp.StatusCode < 200 || resp.StatusCode > 299:
			msg := errorMessage(data)
			if rejected(resp) {
				msg += fmt.Sprintf(" (no usable credential: run `az login --tenant <tenant of the organization>` or `az devops login --organization %s`)", c.BaseURL)
			}
			return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
		}

		if dst == nil {
			return nil
		}
		if err := json.Unmarshal(data, dst); err != nil {
			return fmt.Errorf("%s %s: unexpected response (not JSON): %.80s", method, path, strings.TrimSpace(string(data)))
		}
		return nil
	}
}

func (c *Client) retryDelay(resp *http.Response, attempt int) time.Duration {
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, 30*time.Second)
	}
	return c.backoff << attempt
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// errorMessage pulls the "message" out of an Azure DevOps error body, falling
// back to a trimmed copy of the raw text.
func errorMessage(data []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return e.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// Identity is the signed-in Azure DevOps user.
type Identity struct {
	ID          string
	UniqueName  string // sign-in address, usually the e-mail
	DisplayName string
}

// CurrentUser returns the identity the access token belongs to.
func (c *Client) CurrentUser(ctx context.Context) (Identity, error) {
	var res struct {
		AuthenticatedUser struct {
			ID                  string `json:"id"`
			ProviderDisplayName string `json:"providerDisplayName"`
			Properties          struct {
				Account struct {
					Value string `json:"$value"`
				} `json:"Account"`
			} `json:"properties"`
		} `json:"authenticatedUser"`
	}
	err := c.Get(ctx, "/_apis/connectionData", url.Values{"api-version": {"7.1-preview"}}, &res)
	if err != nil {
		return Identity{}, err
	}
	u := res.AuthenticatedUser
	if u.ID == "" {
		return Identity{}, fmt.Errorf("could not determine the signed-in user")
	}
	return Identity{ID: u.ID, UniqueName: u.Properties.Account.Value, DisplayName: u.ProviderDisplayName}, nil
}

// authorization returns the Authorization header value, plus the bearer token
// it was built from (empty for a PAT) so a rejected token can be invalidated.
func (c *Client) authorization(ctx context.Context) (header, token string, err error) {
	if c.usePAT.Load() {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+c.PAT)), "", nil
	}
	tok, err := c.bearer(ctx)
	if err != nil {
		return "", "", err
	}
	return "Bearer " + tok, tok, nil
}

// rejected reports whether the service refused the credentials. 203 is what
// Azure DevOps answers when it did not accept them.
func rejected(resp *http.Response) bool {
	return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNonAuthoritativeInfo
}
