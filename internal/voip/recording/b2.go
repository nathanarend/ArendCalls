package recording

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// B2Client uploads objects to a Backblaze B2 bucket through its S3-compatible
// API, signing requests with AWS Signature Version 4. Only the two operations
// the recording handoff needs are implemented: PutObject and a presigned GET
// URL. No SDK dependency — the signing is ~120 lines below.
type B2Client struct {
	endpoint string // e.g. https://s3.us-west-004.backblazeb2.com
	region   string // e.g. us-west-004
	bucket   string
	keyID    string
	appKey   string
	http     *http.Client
}

// B2Config is the subset of RecordingConfig needed to talk to B2.
type B2Config struct {
	Endpoint string
	Region   string
	Bucket   string
	KeyID    string
	AppKey   string
}

func NewB2Client(c B2Config) (*B2Client, error) {
	ep := strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	if ep == "" || c.Bucket == "" || c.KeyID == "" || c.AppKey == "" {
		return nil, fmt.Errorf("b2: incomplete config")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	region := strings.TrimSpace(c.Region)
	if region == "" {
		region = regionFromEndpoint(ep)
	}
	return &B2Client{
		endpoint: ep,
		region:   region,
		bucket:   c.Bucket,
		keyID:    c.KeyID,
		appKey:   c.AppKey,
		http:     &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// regionFromEndpoint extracts "us-west-004" from "https://s3.us-west-004.backblazeb2.com".
func regionFromEndpoint(ep string) string {
	u, err := url.Parse(ep)
	if err != nil {
		return "us-east-005"
	}
	parts := strings.Split(u.Host, ".")
	if len(parts) >= 2 && parts[0] == "s3" {
		return parts[1]
	}
	return "us-east-005"
}

const sigService = "s3"

// PutObject uploads body (already fully buffered so its length and hash are
// known) to the given key. contentType may be empty.
func (c *B2Client) PutObject(ctx context.Context, key, contentType string, body []byte) error {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := hex.EncodeToString(sha256sum(body))

	canonURI := "/" + s3Escape(c.bucket) + "/" + s3EscapePath(key)
	u := c.endpoint + canonURI

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	signedHeaders, canonHeaders := c.canonicalHeaders(req)
	canonReq := strings.Join([]string{
		http.MethodPut,
		canonURI,
		"", // no query
		canonHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + c.region + "/" + sigService + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(sha256sum([]byte(canonReq))),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(c.signingKey(dateStamp), []byte(stringToSign)))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.keyID, scope, signedHeaders, signature))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("b2 put %s: %s: %s", key, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// PresignGet returns a time-limited GET URL for key, valid for ttl.
func (c *B2Client) PresignGet(key string, ttl time.Duration) string {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/" + c.region + "/" + sigService + "/aws4_request"

	canonURI := "/" + s3Escape(c.bucket) + "/" + s3EscapePath(key)
	host := hostOf(c.endpoint)

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", c.keyID+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	canonQuery := strings.ReplaceAll(q.Encode(), "+", "%20")

	canonReq := strings.Join([]string{
		http.MethodGet,
		canonURI,
		canonQuery,
		"host:" + host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(sha256sum([]byte(canonReq))),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(c.signingKey(dateStamp), []byte(stringToSign)))

	return c.endpoint + canonURI + "?" + canonQuery + "&X-Amz-Signature=" + signature
}

func (c *B2Client) canonicalHeaders(req *http.Request) (signed, canonical string) {
	host := req.URL.Host
	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	vals := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": req.Header.Get("X-Amz-Content-Sha256"),
		"x-amz-date":           req.Header.Get("X-Amz-Date"),
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		names = append(names, "content-type")
		vals["content-type"] = ct
	}
	sortStrings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(vals[n]))
		b.WriteString("\n")
	}
	return strings.Join(names, ";"), b.String()
}

func (c *B2Client) signingKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+c.appKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(c.region))
	kService := hmacSHA256(kRegion, []byte(sigService))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return endpoint
}

// s3Escape encodes a single path segment per AWS rules (RFC 3986, keeping
// unreserved chars; everything else percent-encoded).
func s3Escape(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if strings.IndexByte(unreserved, ch) >= 0 {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// s3EscapePath escapes each segment of a key but leaves the separating slashes.
func s3EscapePath(key string) string {
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = s3Escape(s)
	}
	return strings.Join(segs, "/")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
