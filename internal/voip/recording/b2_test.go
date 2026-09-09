package recording

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// verifyPutSignature recomputes the SigV4 signature for a received PUT straight
// from the spec, so the test asserts the client's Authorization header actually
// verifies (not just that it is well-shaped). Independent of B2Client internals.
func verifyPutSignature(t *testing.T, r *http.Request, region, keyID, appKey string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	sigIdx := strings.Index(auth, "Signature=")
	if sigIdx < 0 {
		t.Fatalf("no Signature in %q", auth)
	}
	clientSig := auth[sigIdx+len("Signature="):]

	amzDate := r.Header.Get("X-Amz-Date")
	dateStamp := amzDate[:8]
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash != "UNSIGNED-PAYLOAD" {
		t.Fatalf("streaming PUT must sign as UNSIGNED-PAYLOAD, got %q", payloadHash)
	}

	canonHeaders := "content-type:" + r.Header.Get("Content-Type") + "\n" +
		"host:" + r.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonReq := strings.Join([]string{
		http.MethodPut, r.URL.EscapedPath(), "", canonHeaders, signedHeaders, payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/s3/aws4_request"
	crHash := sha256.Sum256([]byte(canonReq))
	sts := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(crHash[:])}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+appKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	want := hex.EncodeToString(hmacSHA256(kSigning, []byte(sts)))

	if clientSig != want {
		t.Fatalf("signature mismatch\n client: %s\n verify: %s\n canonReq:\n%s", clientSig, want, canonReq)
	}
	_ = keyID
}

func TestS3EscapePath(t *testing.T) {
	cases := map[string]string{
		"recordings/clinic 9/2026/09/ab+cd.wav": "recordings/clinic%209/2026/09/ab%2Bcd.wav",
		"simple/key.wav":                        "simple/key.wav",
	}
	for in, want := range cases {
		if got := s3EscapePath(in); got != want {
			t.Errorf("s3EscapePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestB2PutObjectStreams(t *testing.T) {
	payload := []byte("RIFF....fake wav bytes....")

	var gotAuth, gotPath, gotLen string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotLen = r.Header.Get("Content-Length")
		gotBody, _ = io.ReadAll(r.Body)
		verifyPutSignature(t, r, "us-west-004", "0004abc", "K004secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewB2Client(B2Config{
		Endpoint: srv.URL, Region: "us-west-004", Bucket: "clinic-recs",
		KeyID: "0004abc", AppKey: "K004secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.PutObject(context.Background(), "recordings/c1/2026/09/call-x.wav", "audio/wav",
		strings.NewReader(string(payload)), int64(len(payload))); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	if string(gotBody) != string(payload) {
		t.Errorf("body mismatch: got %q", gotBody)
	}
	if gotLen != "26" {
		t.Errorf("Content-Length = %q, want 26", gotLen)
	}
	if gotPath != "/clinic-recs/recordings/c1/2026/09/call-x.wav" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=0004abc/") ||
		!strings.Contains(gotAuth, "/us-west-004/s3/aws4_request") ||
		!strings.Contains(gotAuth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("malformed Authorization: %s", gotAuth)
	}
}

func TestB2HeadObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		if r.Header.Get("X-Amz-Content-Sha256") != emptyPayloadHash {
			t.Errorf("HEAD payload hash = %q", r.Header.Get("X-Amz-Content-Sha256"))
		}
		w.Header().Set("Content-Length", "204844")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := NewB2Client(B2Config{Endpoint: srv.URL, Region: "us-west-004", Bucket: "b", KeyID: "k", AppKey: "s"})
	n, err := c.HeadObject(context.Background(), "recordings/x.wav")
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if n != 204844 {
		t.Errorf("size = %d, want 204844", n)
	}
}

func TestB2HeadObjectMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c, _ := NewB2Client(B2Config{Endpoint: srv.URL, Bucket: "b", KeyID: "k", AppKey: "s"})
	if _, err := c.HeadObject(context.Background(), "nope.wav"); err == nil {
		t.Error("expected error for missing object")
	}
}

func TestB2PresignGet(t *testing.T) {
	c, err := NewB2Client(B2Config{
		Endpoint: "https://s3.us-west-004.backblazeb2.com", Bucket: "b", KeyID: "kid", AppKey: "sec",
	})
	if err != nil {
		t.Fatal(err)
	}
	u := c.PresignGet("recordings/c/2026/09/x.wav", 15*time.Minute)
	for _, want := range []string{
		"https://s3.us-west-004.backblazeb2.com/b/recordings/c/2026/09/x.wav?",
		"X-Amz-Algorithm=AWS4-HMAC-SHA256",
		"X-Amz-Expires=900",
		"X-Amz-SignedHeaders=host",
		"&X-Amz-Signature=",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("presigned URL missing %q\n  got: %s", want, u)
		}
	}
}

func TestB2ClientRejectsIncompleteConfig(t *testing.T) {
	if _, err := NewB2Client(B2Config{Endpoint: "https://x", Bucket: "b"}); err == nil {
		t.Error("expected error for missing credentials")
	}
}
