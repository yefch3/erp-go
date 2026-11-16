package blobstore

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestASCIIFallbackKeepsTheParameterLegal(t *testing.T) {
	// The quoted form has to stay ASCII and must not contain the quote or the
	// backslash, either of which would end the parameter early and let the
	// rest of the file name leak into the header as syntax.
	cases := map[string]string{
		"报价单 2026.pptx":        "___ 2026.pptx",
		`quote "final".pdf`:    "quote _final_.pdf",
		`back\slash.docx`:      "back_slash.docx",
		"ordinary-name_1.xlsx": "ordinary-name_1.xlsx",
	}
	for in, want := range cases {
		if got := asciiFallback(in); got != want {
			t.Errorf("asciiFallback(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestASCIIFallbackNeverReturnsEmpty(t *testing.T) {
	// A name that is entirely non-ASCII would otherwise produce filename="",
	// which some clients treat as "no name" and others as an error.
	if got := asciiFallback("报价单"); got == "" {
		t.Fatal("an all-non-ASCII name must still yield something to save as")
	}
}

// The configurations this system actually runs with. Production's is copied
// from deploy/.env's shape (endpoint, region, bucket spelling), with made-up
// keys: nothing here touches the network.
var shapeConfigs = map[string]Config{
	"production (S3, us-west-2)": {
		Endpoint: "s3.us-west-2.amazonaws.com", PublicEndpoint: "s3.us-west-2.amazonaws.com",
		AccessKey: "AKIAEXAMPLE", SecretKey: "example", UseSSL: true,
		Bucket: "erp-files-564623062079", Region: "us-west-2",
	},
	"docker dev (two endpoints)": {
		Endpoint: "minio:9000", PublicEndpoint: "localhost:19000",
		AccessKey: "erp", SecretKey: "erp_dev_password", Bucket: "erp-files",
	},
	"bare-metal dev (one endpoint)": {
		Endpoint:  "localhost:19000",
		AccessKey: "erp", SecretKey: "erp_dev_password", Bucket: "erp-files",
	},
}

// The property the gateway's file relay rests on: every URL a Store signs for
// browsers is covered by the public shape PresignedShapes reports for the
// same configuration. The relay recognises its own store by that shape, so a
// URL outside it is an upload refused.
func TestPresignedShapeCoversEverythingTheStoreSigns(t *testing.T) {
	ctx := context.Background()
	const key = "travel-reimbursements/4/4/1762-发票 (1).xlsx"
	for name, cfg := range shapeConfigs {
		public, _, err := PresignedShapes(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		store, err := NewOffline(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		signed := map[string]func() (*url.URL, error){
			"PresignedPut":       func() (*url.URL, error) { return store.PresignedPut(ctx, key, 10*time.Minute) },
			"PresignedGet":       func() (*url.URL, error) { return store.PresignedGet(ctx, key, 10*time.Minute) },
			"PresignedGetAs":     func() (*url.URL, error) { return store.PresignedGetAs(ctx, key, "发票.xlsx", 10*time.Minute) },
			"PresignedGetInline": func() (*url.URL, error) { return store.PresignedGetInline(ctx, key, "application/pdf", 10*time.Minute) },
		}
		for method, sign := range signed {
			u, err := sign()
			if err != nil {
				t.Fatalf("%s %s: %v", name, method, err)
			}
			if !public.Covers(u) {
				t.Errorf("%s: %s signed %s://%s%s, outside the reported shape %+v",
					name, method, u.Scheme, u.Host, u.Path, public)
			}
			if !strings.HasSuffix(u.Path, key) {
				t.Errorf("%s: %s path %q does not end with the key", name, method, u.Path)
			}
		}
	}
}

// Pinned on purpose. These are the names the signing client derives today;
// the relay does not depend on them (it asks, see above), but a library
// upgrade that changes them changes what browsers and firewalls see, and that
// should arrive as a failing test with this comment next to it rather than as
// a surprise in production.
func TestPresignedShapesOfTheConfigurationsWeRun(t *testing.T) {
	want := map[string][2]URLShape{
		"production (S3, us-west-2)": {
			{Scheme: "https", Host: "erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com", PathPrefix: "/"},
			{Scheme: "https", Host: "erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com", PathPrefix: "/"},
		},
		"docker dev (two endpoints)": {
			{Scheme: "http", Host: "localhost:19000", PathPrefix: "/erp-files/"},
			{Scheme: "http", Host: "minio:9000", PathPrefix: "/erp-files/"},
		},
		"bare-metal dev (one endpoint)": {
			{Scheme: "http", Host: "localhost:19000", PathPrefix: "/erp-files/"},
			{Scheme: "http", Host: "localhost:19000", PathPrefix: "/erp-files/"},
		},
	}
	for name, cfg := range shapeConfigs {
		public, internal, err := PresignedShapes(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := [2]URLShape{public, internal}; got != want[name] {
			t.Errorf("%s:\n got public %+v internal %+v\nwant public %+v internal %+v",
				name, public, internal, want[name][0], want[name][1])
		}
	}
}

// On Amazon the region is part of the host, so a caller that forgets it gets
// a shape for the wrong region rather than an error. Nothing can detect that
// from inside; the test is here so the dependency is written down somewhere
// that runs.
func TestPresignedShapeDependsOnTheRegion(t *testing.T) {
	cfg := shapeConfigs["production (S3, us-west-2)"]
	right, _, err := PresignedShapes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Region = ""
	wrong, _, err := PresignedShapes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if right.Host == wrong.Host {
		t.Fatalf("the region no longer changes the signed host (%s); the warning on PresignedShapes can go", right.Host)
	}
}

func TestURLShapeCoversOnlyItsOwnStore(t *testing.T) {
	shape := URLShape{Scheme: "http", Host: "localhost:19000", PathPrefix: "/erp-files/"}
	cases := map[string]bool{
		"http://localhost:19000/erp-files/a.jpg":              true,
		"http://LOCALHOST:19000/erp-files/a.jpg":              true, // hosts are case-insensitive
		"https://localhost:19000/erp-files/a.jpg":             false,
		"http://localhost:19001/erp-files/a.jpg":              false,
		"http://localhost:19000/other-bucket/a.jpg":           false,
		"http://localhost:19000/erp-files/":                   false, // the bucket itself, no key
		"http://localhost:19000/erp-files":                    false,
		"http://localhost.evil.example:19000/erp-files/a.jpg": false, // looks like ours, is not
		// U+212A KELVIN SIGN folds to "k" under Unicode case folding. It is
		// not a "k", and this is not our bucket.
		"http://loca\u212Ahost:19000/erp-files/a.jpg": false,
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := shape.Covers(u); got != want {
			t.Errorf("Covers(%s) = %v, want %v", raw, got, want)
		}
	}
	if (URLShape{}).Covers(&url.URL{}) {
		t.Error("the zero shape must cover nothing")
	}
	if shape.Covers(nil) {
		t.Error("a nil URL is not covered")
	}
}

func TestPresignedShapesRefusesAConfigurationThatCannotSign(t *testing.T) {
	if _, _, err := PresignedShapes(Config{Endpoint: "localhost:19000"}); err == nil {
		t.Fatal("a configuration without a bucket produced a shape")
	}
}
