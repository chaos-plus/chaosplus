package geoip

import (
	"context"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	geoiplib "github.com/chaos-plus/chaosplus/pkg/geoip"
)

// RegisterREST mounts the geoip lookup endpoints on the huma API. This makes the
// module a full feature unit (like the guid module), not just background work.
func (m *Module) RegisterREST(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "lookup-geoip-self",
		Method:        http.MethodGet,
		Path:          "/geoip",
		Summary:       "Look up the geolocation of the caller's own IP",
		Description: "Detects the client's IPv4 address from X-Forwarded-For/X-Real-IP and " +
			"the transport peer, preferring a public address (public > private > loopback), " +
			"and redirects to GET /geoip/{ip}. IPv6 loopback maps to 127.0.0.1.",
		DefaultStatus: http.StatusTemporaryRedirect,
		Tags:          []string{"geoip"},
	}, lookupSelf)

	huma.Register(api, huma.Operation{
		OperationID: "lookup-geoip",
		Method:      http.MethodGet,
		Path:        "/geoip/{ip}",
		Summary:     "Look up the geolocation of an IP address",
		Description: "Returns country/province/city for the given IP from every " +
			"provider that resolves it, one entry per provider.",
		Tags: []string{"geoip"},
	}, lookupGeoIP)
}

// selfInput captures the client's best IPv4 address via a resolver, since it is
// derived from headers and the transport peer rather than a normal request
// parameter.
type selfInput struct {
	clientIP string
}

func (i *selfInput) Resolve(ctx huma.Context) []error {
	i.clientIP = detectClientIPv4(ctx)
	return nil
}

// redirectOutput carries only a Location header; the 307 status comes from the
// operation's DefaultStatus.
type redirectOutput struct {
	Location string `header:"Location"`
}

// lookupSelf redirects the caller to the lookup for its own detected IPv4.
func lookupSelf(_ context.Context, in *selfInput) (*redirectOutput, error) {
	if in.clientIP == "" {
		return nil, huma.Error400BadRequest("could not determine an IPv4 address for the client")
	}
	return &redirectOutput{Location: "/geoip/" + in.clientIP}, nil
}

// lookupInput is the request for GET /geoip/{ip}.
type lookupInput struct {
	IP string `path:"ip" example:"8.8.8.8" doc:"the IPv4 address (x.x.x.x) to look up"`
}

// Resolve rejects anything that is not a valid IPv4 address so providers never
// receive garbage input. huma turns the returned detail into a 422 with a
// path.ip location pointer.
func (i *lookupInput) Resolve(_ huma.Context) []error {
	if !isIPv4(i.IP) {
		return []error{&huma.ErrorDetail{
			Message:  "not a valid IPv4 address (expected x.x.x.x)",
			Location: "path.ip",
			Value:    i.IP,
		}}
	}
	return nil
}

// lookupGeoIP resolves an IP via the process-wide provider registry, returning
// one entry per provider that resolves it.
func lookupGeoIP(ctx context.Context, in *lookupInput) (*respx.Body[[]*geoiplib.GeoIp], error) {
	infos, err := geoiplib.GetIpLocations(in.IP)
	if err != nil {
		return nil, huma.Error404NotFound("no geolocation for ip", err)
	}
	if len(infos) == 0 {
		return nil, huma.Error404NotFound("no geolocation for ip")
	}
	return respx.OK(ctx, infos), nil
}

// hostOnly strips the port from a "host:port" remote address.
func hostOnly(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
