package fasthttpadaptor

import (
	"bufio"
	"net/http"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
)

func TestConvertRequestPreservesDuplicateHeaders(t *testing.T) {
	var ctx fasthttp.RequestCtx
	var req fasthttp.Request

	req.Header.SetMethod("GET")
	req.SetRequestURI("/")
	req.Header.SetHost("example.com")
	req.Header.Add("X-Forwarded-For", "10.0.0.1")
	req.Header.Add("X-Forwarded-For", "203.0.113.7")
	ctx.Init(&req, nil, nil)

	var r http.Request
	if err := ConvertRequest(&ctx, &r, true); err != nil {
		t.Fatalf("ConvertRequest returned error: %v", err)
	}

	got := r.Header.Values("X-Forwarded-For")
	want := []string{"10.0.0.1", "203.0.113.7"}
	if len(got) != len(want) {
		t.Fatalf("X-Forwarded-For = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("X-Forwarded-For = %q, want %q", got, want)
		}
	}
}

func BenchmarkConvertRequest(b *testing.B) {
	var httpReq http.Request

	ctx := &fasthttp.RequestCtx{
		Request: fasthttp.Request{
			Header:        fasthttp.RequestHeader{},
			UseHostHeader: false,
		},
	}
	ctx.Request.Header.SetMethod("GET")
	ctx.Request.Header.Set("x", "test")
	ctx.Request.Header.Set("y", "test")
	ctx.Request.SetRequestURI("/test")
	ctx.Request.SetHost("test")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ConvertRequest(ctx, &httpReq, true)
	}
}

func TestConvertRequestProtocolVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		protocol, wantProto  string
		wantMajor, wantMinor int
	}{
		{"HTTP/2", "HTTP/2.0", 2, 0},
		{"HTTP/1.0", "HTTP/1.0", 1, 0},
		{"HTTP/1.1", "HTTP/1.1", 1, 1},
	} {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.Header.SetMethod(fasthttp.MethodGet)
		ctx.Request.SetRequestURI("/")
		ctx.Request.Header.SetHost("example.com")
		ctx.Request.Header.SetProtocol(tc.protocol)

		var r http.Request
		if err := ConvertRequest(ctx, &r, true); err != nil {
			t.Fatalf("ConvertRequest(%s) error: %v", tc.protocol, err)
		}
		if r.Proto != tc.wantProto || r.ProtoMajor != tc.wantMajor || r.ProtoMinor != tc.wantMinor {
			t.Errorf("%s -> %q %d.%d, want %q %d.%d",
				tc.protocol, r.Proto, r.ProtoMajor, r.ProtoMinor, tc.wantProto, tc.wantMajor, tc.wantMinor)
		}
	}
}

// A reused destination request must not inherit its predecessor's trailers.
func TestConvertRequestResetsTrailerOnReuse(t *testing.T) {
	t.Parallel()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(fasthttp.MethodPost)
	ctx.Request.SetRequestURI("/")
	ctx.Request.Header.SetHost("example.com")
	if err := ctx.Request.Header.SetTrailer("X-T"); err != nil {
		t.Fatal(err)
	}
	ctx.Request.Header.Set("X-T", "first")

	var r http.Request
	if err := ConvertRequest(ctx, &r, true); err != nil {
		t.Fatal(err)
	}
	if got := r.Trailer.Get("X-T"); got != "first" {
		t.Fatalf("Trailer[X-T] = %q, want first", got)
	}

	plain := &fasthttp.RequestCtx{}
	plain.Request.Header.SetMethod(fasthttp.MethodGet)
	plain.Request.SetRequestURI("/")
	plain.Request.Header.SetHost("example.com")
	if err := ConvertRequest(plain, &r, true); err != nil {
		t.Fatal(err)
	}
	if r.Trailer != nil {
		t.Fatalf("Trailer = %v after a trailer-free request, want nil", r.Trailer)
	}
}

// An unannounced field from the trailer section still lands in Request.Trailer.
func TestConvertRequestCarriesUnannouncedTrailer(t *testing.T) {
	t.Parallel()

	raw := "POST / HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"4\r\nbody\r\n0\r\nX-Late: v\r\n\r\n"
	ctx := &fasthttp.RequestCtx{}
	if err := ctx.Request.Read(bufio.NewReader(strings.NewReader(raw))); err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	var r http.Request
	if err := ConvertRequest(ctx, &r, true); err != nil {
		t.Fatal(err)
	}
	if got := r.Trailer.Get("X-Late"); got != "v" {
		t.Fatalf("Trailer[X-Late] = %q, want v", got)
	}
	if v := r.Header.Values("X-Late"); v != nil {
		t.Fatalf("Header[X-Late] = %q, want the trailer out of the header map", v)
	}
}

func TestConvertRequestCarriesTrailers(t *testing.T) {
	t.Parallel()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(fasthttp.MethodPost)
	ctx.Request.SetRequestURI("/")
	ctx.Request.Header.SetHost("example.com")
	ctx.Request.SetBodyString("body")
	if err := ctx.Request.Header.SetTrailer("Foo"); err != nil {
		t.Fatalf("SetTrailer() error: %v", err)
	}
	ctx.Request.Header.Add("Foo", "one")
	ctx.Request.Header.Add("Foo", "two")

	var r http.Request
	if err := ConvertRequest(ctx, &r, true); err != nil {
		t.Fatalf("ConvertRequest() error: %v", err)
	}
	got := r.Trailer.Values("Foo")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("Trailer[Foo] = %q, want [one two]", got)
	}
	if v := r.Header.Values("Trailer"); v != nil {
		t.Errorf("Header[Trailer] = %q, want the announcement out of the header map", v)
	}
	if v := r.Header.Values("Foo"); v != nil {
		t.Errorf("Header[Foo] = %q, want announced trailers out of the header map", v)
	}
}
