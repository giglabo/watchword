package proxy

import "time"

// URLSigner generates HMAC-signed proxy URLs for uploads and downloads.
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

func (s *URLSigner) SignDownload(entryID, filename string) string {
	return SignDownloadURL(s.baseURL, s.secret, entryID, filename, s.ttl)
}

func (s *URLSigner) SignUpload(entryID, filename string) string {
	return SignUploadURL(s.baseURL, s.secret, entryID, filename, s.ttl)
}
