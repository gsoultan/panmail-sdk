package panmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	panmail "github.com/gsoultan/panmail-sdk"
)

// page is one ListEmailProviders response: the providers, and the token for the
// next page, if there is one.
func page(next string, providers ...map[string]any) map[string]any {
	body := map[string]any{"providers": providers}
	if next != "" {
		body["nextPageToken"] = next
	}
	return body
}

// provider is one entry in a page, holding only the fields a sender needs.
func provider(id, kind string) map[string]any {
	return map[string]any{"id": id, "name": "provider " + id, "type": kind}
}

func lists(t *testing.T, pages ...map[string]any) *gateway {
	t.Helper()
	return respond(t, func(w http.ResponseWriter, call int) {
		writeJSON(w, http.StatusOK, pages[min(call, len(pages))-1])
	})
}

func ids(providers []panmail.Provider) []string {
	out := []string{}
	for _, p := range providers {
		out = append(out, p.ID)
	}
	return out
}

func TestListProviders(t *testing.T) {
	g := lists(t, page("",
		map[string]any{
			"id":             "3f1c2b7a-0000-4000-8000-000000000001",
			"name":           "Production SES",
			"type":           "PROVIDER_TYPE_SES",
			"allowedDomains": []string{"example.com", "example.org"},
		},
		map[string]any{
			"id":   "3f1c2b7a-0000-4000-8000-000000000002",
			"name": "Fallback SMTP",
			"type": "PROVIDER_TYPE_SMTP",
		},
	))

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	want := []panmail.Provider{
		{
			ID:             "3f1c2b7a-0000-4000-8000-000000000001",
			Name:           "Production SES",
			Type:           panmail.ProviderTypeSES,
			AllowedDomains: []string{"example.com", "example.org"},
		},
		{
			ID:   "3f1c2b7a-0000-4000-8000-000000000002",
			Name: "Fallback SMTP",
			Type: panmail.ProviderTypeSMTP,
		},
	}
	if !reflect.DeepEqual(providers, want) {
		t.Errorf("ListProviders = %+v, want %+v", providers, want)
	}
}

// The same procedure the dashboard's Email Providers page reads from, with the
// key where every call puts it.
func TestListProvidersPostsToTheConnectProcedure(t *testing.T) {
	g := lists(t, page(""))

	if _, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{}); err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	r := g.requests[0]
	if got := r.URL.Path; got != "/panmail.v1.EmailProviderService/ListEmailProviders" {
		t.Errorf("path = %q, want /panmail.v1.EmailProviderService/ListEmailProviders", got)
	}
	if r.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", r.Method)
	}
	if got := r.Header.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q, want test-key", got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want it left unset", got)
	}
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// An empty filter field is left out, not sent as "": the gateway decodes an
// enum name it does not know as no filter, and an empty string is one.
func TestListProvidersAsksForAPageAndNoFilterByDefault(t *testing.T) {
	g := lists(t, page(""))

	if _, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{}); err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	want := map[string]any{"pageSize": float64(50)}
	if !reflect.DeepEqual(g.bodies[0], want) {
		t.Errorf("request = %v, want %v", g.bodies[0], want)
	}
}

func TestListProvidersSendsTheFilter(t *testing.T) {
	g := lists(t, page(""))

	filter := panmail.ProviderFilter{Name: "prod", Type: panmail.ProviderTypeSMTP}
	if _, err := client(t, g).ListProviders(context.Background(), filter); err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	want := map[string]any{"pageSize": float64(50), "name": "prod", "type": "PROVIDER_TYPE_SMTP"}
	if !reflect.DeepEqual(g.bodies[0], want) {
		t.Errorf("request = %v, want %v", g.bodies[0], want)
	}
}

// ProviderTypeUnspecified is the gateway's own "no filter". Sending it would be
// harmless, but checking the answer against it would keep nothing.
func TestListProvidersWithUnspecifiedTypeListsEveryKind(t *testing.T) {
	g := lists(t, page("", provider("a", "PROVIDER_TYPE_SES"), provider("b", "PROVIDER_TYPE_IMAP")))

	filter := panmail.ProviderFilter{Type: panmail.ProviderTypeUnspecified}
	providers, err := client(t, g).ListProviders(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if _, sent := g.bodies[0]["type"]; sent {
		t.Errorf("request = %v, want no type in it", g.bodies[0])
	}
	if got := ids(providers); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("providers = %v, want [a b]", got)
	}
}

func TestListProvidersFollowsEveryPage(t *testing.T) {
	g := lists(t,
		page("NTA=", provider("a", "PROVIDER_TYPE_SES"), provider("b", "PROVIDER_TYPE_SES")),
		page("MTAw", provider("c", "PROVIDER_TYPE_SES")),
		page("", provider("d", "PROVIDER_TYPE_SES")),
	)

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{Name: "prod"})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if got := ids(providers); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("providers = %v, want [a b c d], in the order the pages gave them", got)
	}
	if g.calls != 3 {
		t.Fatalf("gateway called %d times, want 3", g.calls)
	}

	// The token goes back exactly as it came, and the filter rides along on
	// every page rather than only the first.
	for i, token := range []any{nil, "NTA=", "MTAw"} {
		if got := g.bodies[i]["pageToken"]; got != token {
			t.Errorf("page %d asked with pageToken %v, want %v", i+1, got, token)
		}
		if got := g.bodies[i]["name"]; got != "prod" {
			t.Errorf("page %d asked with name %v, want prod", i+1, got)
		}
	}
}

// The gateway pages by offset, newest first. A provider created between two
// requests moves every row down one, and the last of one page arrives again
// as the first of the next.
func TestListProvidersReturnsAProviderRepeatedAcrossPagesOnce(t *testing.T) {
	g := lists(t,
		page("Mg==", provider("a", "PROVIDER_TYPE_SES"), provider("b", "PROVIDER_TYPE_SES")),
		page("", provider("b", "PROVIDER_TYPE_SES"), provider("c", "PROVIDER_TYPE_SES")),
	)

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if got := ids(providers); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("providers = %v, want [a b c]", got)
	}
}

// The gateway hands out a token for any full page, so a list that is an exact
// multiple of the page size ends with an empty one.
func TestListProvidersTreatsAnEmptyLastPageAsTheEnd(t *testing.T) {
	g := lists(t, page("MQ==", provider("a", "PROVIDER_TYPE_SES")), page(""))

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if got := ids(providers); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("providers = %v, want [a]", got)
	}
	if g.calls != 2 {
		t.Errorf("gateway called %d times, want 2", g.calls)
	}
}

// A token that comes round again is a loop, and following it would read the
// same pages until the page limit — then report that instead of the loop.
func TestListProvidersRefusesATokenItHasAlreadyFollowed(t *testing.T) {
	g := lists(t,
		page("A", provider("a", "PROVIDER_TYPE_SES")),
		page("B", provider("b", "PROVIDER_TYPE_SES")),
		page("A", provider("a", "PROVIDER_TYPE_SES")),
	)

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err == nil {
		t.Fatalf("ListProviders = %v, want an error", ids(providers))
	}
	if !strings.Contains(err.Error(), "a second time") {
		t.Errorf("error = %q, want it to say the token repeated", err)
	}
	if providers != nil {
		t.Errorf("providers = %v, want none: a partial list reads as a complete one", ids(providers))
	}
	if g.calls != 3 {
		t.Errorf("gateway called %d times, want 3", g.calls)
	}
}

// A gateway that never stops handing out fresh tokens is stopped by the page
// limit, and the answer is an error rather than the pages read so far: a list
// cut short that looks complete is how a caller concludes a provider does not
// exist.
func TestListProvidersStopsAtThePageLimit(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, call int) {
		writeJSON(w, http.StatusOK, page(fmt.Sprintf("t%d", call), provider(fmt.Sprintf("p%d", call), "PROVIDER_TYPE_SES")))
	})

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err == nil {
		t.Fatalf("ListProviders returned %d providers, want an error", len(providers))
	}
	if !strings.Contains(err.Error(), "still paging") {
		t.Errorf("error = %q, want it to say the gateway kept paging", err)
	}
	if providers != nil {
		t.Errorf("providers = %d of them, want none", len(providers))
	}
	if g.calls != 200 {
		t.Errorf("gateway called %d times, want 200", g.calls)
	}
}

// The gateway decodes an enum name it does not recognise as no filter at all,
// so a misspelt type comes back as every provider. Checking the answer is what
// makes a filter mean what it says.
func TestListProvidersKeepsOnlyTheTypeItAskedFor(t *testing.T) {
	ignoresTheFilter := page("",
		provider("ses", "PROVIDER_TYPE_SES"),
		provider("smtp", "PROVIDER_TYPE_SMTP"),
		provider("imap", "PROVIDER_TYPE_IMAP"),
	)

	cases := map[string]struct {
		filter panmail.ProviderType
		want   []string
	}{
		"a type it knows":   {filter: panmail.ProviderTypeSMTP, want: []string{"smtp"}},
		"a misspelt type":   {filter: panmail.ProviderType("SMTP"), want: []string{}},
		"a type none match": {filter: panmail.ProviderTypeMailgun, want: []string{}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := lists(t, ignoresTheFilter)

			providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{Type: tc.filter})
			if err != nil {
				t.Fatalf("ListProviders: %v", err)
			}
			if got := ids(providers); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("providers = %v, want %v", got, tc.want)
			}
		})
	}
}

// The gateway returns every provider's configuration — credentials cleared —
// along with its tenant id, timestamps and send ceilings. None of it is needed
// to send, and none of it is decoded, so a lapse in the gateway's clearing has
// nowhere here to land. The password below is what such a lapse would look
// like.
func TestListProvidersDecodesOnlyWhatASenderNeeds(t *testing.T) {
	g := lists(t, page("", map[string]any{
		"id":                "p1",
		"name":              "Relay",
		"type":              "PROVIDER_TYPE_SMTP",
		"allowedDomains":    []string{"example.com"},
		"tenantId":          "tenant-7c1e",
		"sendRatePerMinute": 600,
		"sendBurst":         100,
		"createTime":        "2026-09-01T10:00:00Z",
		"updateTime":        "2026-09-01T10:00:00Z",
		"webhookSecret":     "whsec-lapse",
		"smtp": map[string]any{
			"host":     "smtp.internal.example",
			"port":     587,
			"username": "relay-user",
			"password": "hunter2",
			"dkim":     map[string]any{"domain": "example.com", "selector": "s1", "privateKey": "-----BEGIN"},
		},
	}))

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	want := []panmail.Provider{{
		ID: "p1", Name: "Relay", Type: panmail.ProviderTypeSMTP, AllowedDomains: []string{"example.com"},
	}}
	if !reflect.DeepEqual(providers, want) {
		t.Errorf("ListProviders = %+v, want %+v", providers, want)
	}

	logged := fmt.Sprintf("%+v %#v", providers, providers)
	for _, leak := range []string{"hunter2", "-----BEGIN", "whsec-lapse", "relay-user", "smtp.internal.example", "tenant-7c1e"} {
		if strings.Contains(logged, leak) {
			t.Errorf("a logged provider carries %q", leak)
		}
	}
}

// protojson writes an enum value its descriptor has no name for as a bare
// number, and provider_type.proto reserves 2 to 5 because stored rows may still
// carry them. One such row must not fail the list.
func TestListProvidersKeepsATypeTheGatewayHasNoNameFor(t *testing.T) {
	g := lists(t, page("",
		map[string]any{"id": "old", "name": "Old", "type": 3},
		provider("new", "PROVIDER_TYPE_SES"),
	))

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if len(providers) != 2 {
		t.Fatalf("got %d providers, want 2", len(providers))
	}
	if got := providers[0].Type; got != "3" {
		t.Errorf("Type = %q, want its decimal spelling, 3", got)
	}
}

// protobuf JSON leaves a zero value out, so a provider with no type in the body
// is one whose type is UNSPECIFIED — not one with a type of "".
func TestListProvidersReadsAMissingTypeAsUnspecified(t *testing.T) {
	g := lists(t, page("", map[string]any{"id": "p1", "name": "Nameless kind"}))

	providers, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}

	if got := providers[0].Type; got != panmail.ProviderTypeUnspecified {
		t.Errorf("Type = %q, want %q", got, panmail.ProviderTypeUnspecified)
	}
}

func TestListProvidersRefusesATypeThatIsNeitherANameNorANumber(t *testing.T) {
	g := lists(t, page("", map[string]any{"id": "p1", "type": map[string]any{"smtp": true}}))

	if _, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{}); err == nil {
		t.Fatal("ListProviders accepted a type that is an object")
	}
}

// A key minted for sending holds email:send alone. The gateway names the scope
// it is missing, and that is the message worth passing on.
func TestListProvidersNeedsTheProvidersReadScope(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusForbidden, "permission_denied", `api key is missing the "providers:read" scope`)
	})

	_, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})

	var authErr *panmail.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v (%T), want *AuthError", err, err)
	}
	if !strings.Contains(err.Error(), "providers:read") {
		t.Errorf("error = %q, want it to name the missing scope", err)
	}
}

// A full queue, a suppressed recipient and a rate limit are answers to a send.
// Read into the same code from a listing they would be lies, and the rate limit
// would be waited out for a call the gateway does not limit at all.
func TestListProvidersDoesNotReadSendRefusalsIntoAList(t *testing.T) {
	cases := map[string]struct {
		status  int
		code    string
		headers map[string]string
	}{
		"failed_precondition":                   {status: http.StatusBadRequest, code: "failed_precondition"},
		"resource_exhausted without a delay":    {status: http.StatusTooManyRequests, code: "resource_exhausted"},
		"resource_exhausted with a delay":       {status: http.StatusTooManyRequests, code: "resource_exhausted", headers: map[string]string{"Retry-After": "1"}},
		"a 429 from a proxy, carrying no code":  {status: http.StatusTooManyRequests},
		"invalid_argument, which it always was": {status: http.StatusBadRequest, code: "invalid_argument"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := respond(t, func(w http.ResponseWriter, _ int) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				if tc.code == "" {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte("<html>slow down</html>"))
					return
				}
				refuse(w, tc.status, tc.code, "no")
			})

			_, err := client(t, g, panmail.WithRateLimitRetries(3)).
				ListProviders(context.Background(), panmail.ProviderFilter{})

			var apiErr *panmail.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v (%T), want an *APIError", err, err)
			}
			var limited *panmail.RateLimitedError
			var backlog *panmail.BacklogFullError
			var suppressed *panmail.SuppressedRecipientError
			if errors.As(err, &limited) || errors.As(err, &backlog) || errors.As(err, &suppressed) {
				t.Errorf("error is %T, a send's refusal; want a plain *APIError", err)
			}
			if apiErr.Code != tc.code || apiErr.Status != tc.status {
				t.Errorf("APIError = %q/%d, want %q/%d", apiErr.Code, apiErr.Status, tc.code, tc.status)
			}
			if g.calls != 1 {
				t.Errorf("gateway called %d times, want 1: nothing on a listing is waited out", g.calls)
			}
		})
	}
}

// A transport error names the call it interrupted. "The send did not complete"
// from a listing would send the reader looking for a message.
func TestListProvidersSaysWhichCallDidNotComplete(t *testing.T) {
	broken := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})}
	c, err := panmail.New("https://mail.example.com", "test-key", panmail.WithHTTPClient(broken))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.ListProviders(context.Background(), panmail.ProviderFilter{})
	if err == nil {
		t.Fatal("ListProviders succeeded without a transport")
	}
	if !strings.Contains(err.Error(), "listing providers did not complete") {
		t.Errorf("error = %q, want it to name the listing", err)
	}
}

// A success larger than the response bound is reported as that. Cut short and
// handed to the decoder, it read as JSON that would not parse, which sends the
// reader looking for a malformed body rather than a long one.
func TestListProvidersReportsAPageOverTheResponseBound(t *testing.T) {
	domains := make([]string, 0, 60_000)
	for i := range cap(domains) {
		domains = append(domains, fmt.Sprintf("customer-%05d.example", i))
	}
	g := lists(t, page("", map[string]any{"id": "p1", "type": "PROVIDER_TYPE_SMTP", "allowedDomains": domains}))

	_, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err == nil {
		t.Fatal("ListProviders accepted a page over the bound")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("error = %q, want it to say the response was too large", err)
	}
}

// The bound is about a success that cannot decode. A refusal whose body runs
// past it — a proxy's error page, say — is still classified by its status, as it
// was before the bound learned to tell the two apart.
func TestARefusalOverTheResponseBoundIsStillClassified(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("<p>forbidden</p>", 100_000)))
	})

	_, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})

	var authErr *panmail.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %.120v (%T), want *AuthError", err, err)
	}
}

// The gateway's enum is the source of truth and the fixture is generated from
// it by scripts/sync-status.py, as for Status.
func TestProviderTypeConstantsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "provider-types.json"))
	if err != nil {
		t.Fatalf("reading the shared fixture: %v", err)
	}
	var fixture struct {
		Constants map[string]string `json:"constants"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the shared fixture: %v", err)
	}

	exported := map[string]panmail.ProviderType{
		"UNSPECIFIED": panmail.ProviderTypeUnspecified,
		"SMTP":        panmail.ProviderTypeSMTP,
		"IMAP":        panmail.ProviderTypeIMAP,
		"POP3":        panmail.ProviderTypePOP3,
		"SENDGRID":    panmail.ProviderTypeSendGrid,
		"SES":         panmail.ProviderTypeSES,
		"POSTMARK":    panmail.ProviderTypePostmark,
		"MAILGUN":     panmail.ProviderTypeMailgun,
	}

	for name, want := range fixture.Constants {
		got, ok := exported[name]
		if !ok {
			t.Errorf("the gateway has %s and this client does not", name)
			continue
		}
		if string(got) != want {
			t.Errorf("ProviderType %s = %q, want %q", name, got, want)
		}
	}
	for name := range exported {
		if _, ok := fixture.Constants[name]; !ok {
			t.Errorf("this client exposes %s and the gateway does not", name)
		}
	}
}
