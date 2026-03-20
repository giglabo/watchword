package proxy

import "time"

// URLSigner generates HMAC-signed proxy download URLs.
// It implements service.ProxyURLSigner.
type URLSigner struct {
	baseURL string
	secret  string
	ttl     time.Duration
}

func NewURLSigner(baseURL, secret string, ttlMinutes int) *URLSigner {
	return &URLSigner{
		baseURL: baseURL,
		secret:  secret,
		ttl:     time.Duration(ttlMinutes) * time.Minute,
	}
}

func (s *URLSigner) Sign(entryID, filename string) string {
	return SignURL(s.baseURL, s.secret, entryID, filename, s.ttl)
}
