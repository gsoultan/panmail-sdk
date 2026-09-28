package panmail

import (
	"context"
	"encoding/json"
	"fmt"
)

// listProvidersProcedure is the Connect route for
// EmailProviderService.ListEmailProviders, the procedure the dashboard's Email
// Providers page reads from.
const listProvidersProcedure = "/panmail.v1.EmailProviderService/ListEmailProviders"

// providerPageSize is how many providers each request asks for. ListProviders
// returns the whole list either way, so this decides only how many round trips
// that takes and how much of maxResponseBytes each provider may use: about 20
// KB apiece, which one exceeds only with an AllowedDomains list running to
// around a thousand names.
const providerPageSize = 50

// maxProviderPages bounds how long ListProviders follows the gateway's page
// tokens. It is there for a gateway, or something in front of one, that never
// stops handing them out: without it the loop is unbounded, and so is the
// memory holding what it has read. At providerPageSize a page that is 10,000
// providers, which is a platform rather than a tenant.
const maxProviderPages = 200

// A listing is not a send, so a code reads as what it says and nothing more:
// no full queue, no suppressed recipient, and no rate limit to wait out — the
// gateway limits sending, not reading.
var listProvidersCall = call{procedure: listProvidersProcedure, what: "listing providers", classify: classify}

// ProviderType is the kind of provider: which vendor, or which protocol, mail
// goes through.
//
// Like Status, the values are the gateway's own enum names, carried verbatim.
// It is a string rather than a closed set because the gateway adds kinds — four
// API vendors arrived at once — and a kind this package has no constant for
// still comes through as what the gateway sent, rather than failing the call or
// being mistaken for another.
type ProviderType string

const (
	ProviderTypeSMTP     ProviderType = "PROVIDER_TYPE_SMTP"
	ProviderTypeSendGrid ProviderType = "PROVIDER_TYPE_SENDGRID"
	ProviderTypeSES      ProviderType = "PROVIDER_TYPE_SES"
	ProviderTypePostmark ProviderType = "PROVIDER_TYPE_POSTMARK"
	ProviderTypeMailgun  ProviderType = "PROVIDER_TYPE_MAILGUN"

	// ProviderTypeIMAP and ProviderTypePOP3 are mailboxes inbound mail is read
	// from, not something to send through; the gateway passes over them when
	// it picks a provider to send with. They are listed because the gateway
	// lists them, so check Type before offering a provider as a sender.
	ProviderTypeIMAP ProviderType = "PROVIDER_TYPE_IMAP"
	ProviderTypePOP3 ProviderType = "PROVIDER_TYPE_POP3"

	// ProviderTypeUnspecified is protobuf's zero value, not a kind of
	// provider. As a filter it means no filter. It is also what a provider
	// sent with no type reads as, because protobuf JSON leaves a zero value
	// out rather than spelling it.
	ProviderTypeUnspecified ProviderType = "PROVIDER_TYPE_UNSPECIFIED"
)

// Provider is a configured provider: as much of one as a sender needs.
//
// That is deliberately not all of it. The gateway also returns the transport
// configuration — host, username, region, the SES access key id — with every
// credential already cleared. None of it is needed to send, and leaving it out
// means a lapse in that clearing has nowhere in this package to land: a
// Provider holds nothing that needs keeping out of a log.
type Provider struct {
	// ID is what Message.ProviderID takes.
	ID string

	// Name is the one shown on the Email Providers page.
	Name string

	Type ProviderType

	// AllowedDomains are the From domains the provider will send as, each
	// compared whole and ignoring case: example.com does not admit
	// mail.example.com. Empty means the operator has not restricted it.
	AllowedDomains []string
}

// ProviderFilter narrows ListProviders. The zero value lists every provider.
type ProviderFilter struct {
	// Name keeps providers whose name contains it, ignoring case: "prod"
	// matches "Production" and "eu-prod". It is a search, not a lookup — to
	// find one provider by name, compare the names that come back. The gateway
	// matches it with SQL LIKE and does not escape it, so % and _ in it are
	// wildcards.
	Name string

	// Type keeps providers of one kind. Empty, or ProviderTypeUnspecified, is
	// every kind.
	Type ProviderType
}

// listProvidersRequest is the wire form of a request for one page. The field
// names are the protobuf JSON names of ListEmailProvidersRequest.
type listProvidersRequest struct {
	PageSize  int          `json:"pageSize"`
	PageToken string       `json:"pageToken,omitempty"`
	Name      string       `json:"name,omitempty"`
	Type      ProviderType `json:"type,omitempty"`
}

// listProvidersResponse is the part of ListEmailProvidersResponse this package
// reads. Each provider also carries its configuration, timestamps, send
// ceilings and tenant id; none of that is named here, so none of it is decoded.
type listProvidersResponse struct {
	Providers []struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		Type           enumName `json:"type"`
		AllowedDomains []string `json:"allowedDomains"`
	} `json:"providers"`
	NextPageToken string `json:"nextPageToken"`
}

// enumName reads an enum from protobuf JSON, where it is normally a name. A
// value the gateway's enum has no name for is written as a bare number instead,
// and provider_type.proto reserves 2 to 5 because stored rows may still carry
// them. Refusing a number would fail the whole list over one old row, so it is
// kept as its decimal spelling: not a constant this package has, and not
// mistaken for one.
type enumName string

func (e *enumName) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		*e = enumName(name)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("an enum is a name or a number, not %s", data)
	}
	*e = enumName(number.String())
	return nil
}

// ListProviders returns every provider in the key's tenant, newest first: the
// ids Message.ProviderID takes, and the From domains each will send as.
//
// The key needs the providers:read scope, which no key has by default. One
// minted for sending holds email:send alone and is refused with an AuthError
// naming the scope. Granting it to a sending key lets that key read every
// provider's configuration from the gateway, credentials excepted; if provider
// ids are only needed while an application is set up, a second key for that
// job keeps the sending key as narrow as it was.
//
// It is one call because providers are configuration rather than a feed: the
// gateway is asked for pages and they are joined here. It pages by offset, so
// a provider created meanwhile can arrive twice — it is returned once — and one
// deleted meanwhile can push another across a page boundary and out of the
// list. Both need the list to span pages and to change while it is read.
//
// Nothing is retried. Listing changes nothing, so any error is safe to repeat
// by calling again.
func (c *Client) ListProviders(ctx context.Context, filter ProviderFilter) ([]Provider, error) {
	want := filter.Type
	if want == ProviderTypeUnspecified {
		want = ""
	}
	req := listProvidersRequest{PageSize: providerPageSize, Name: filter.Name, Type: want}

	var providers []Provider
	seen := map[string]bool{}
	followed := map[string]bool{}

	for range maxProviderPages {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("panmail: encoding the request failed: %w", err)
		}

		var page listProvidersResponse
		if err := c.post(ctx, listProvidersCall, body, &page); err != nil {
			return nil, err
		}

		for _, p := range page.Providers {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true

			kind := ProviderType(p.Type)
			if kind == "" {
				kind = ProviderTypeUnspecified
			}
			// Checked again here because the gateway ignores a type name it
			// does not recognise rather than refusing it: a misspelt filter
			// comes back as every provider, and has to be told apart from one
			// that matched them all.
			if want != "" && kind != want {
				continue
			}

			providers = append(providers, Provider{
				ID:             p.ID,
				Name:           p.Name,
				Type:           kind,
				AllowedDomains: p.AllowedDomains,
			})
		}

		if page.NextPageToken == "" {
			return providers, nil
		}
		if followed[page.NextPageToken] {
			return nil, fmt.Errorf("panmail: the gateway handed out page token %q a second time; "+
				"refusing to read the same pages again", page.NextPageToken)
		}
		followed[page.NextPageToken] = true
		req.PageToken = page.NextPageToken
	}

	return nil, fmt.Errorf("panmail: the gateway was still paging after %d pages of %d providers; "+
		"narrow the list with a name or type filter", maxProviderPages, providerPageSize)
}
