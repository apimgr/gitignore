// Package urlutil provides shared URL construction helpers that guarantee
// all user-supplied input is percent-encoded before it reaches an HTTP
// request. Never build request URLs with raw fmt.Sprintf + user input.
package urlutil

import (
	"net/url"
	"strings"
)

// BuildAPIURL constructs a properly encoded API URL from a base URL, a path
// template containing {placeholder} segments, and optional path/query
// parameter maps. Always use this instead of fmt.Sprintf with user input.
func BuildAPIURL(baseURL, path string, pathParams map[string]string, queryParams map[string]string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	encodedPath := path
	for key, value := range pathParams {
		placeholder := "{" + key + "}"
		encodedPath = strings.ReplaceAll(encodedPath, placeholder, url.PathEscape(value))
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + encodedPath

	if len(queryParams) > 0 {
		q := u.Query()
		for key, value := range queryParams {
			q.Set(key, value)
		}
		u.RawQuery = q.Encode()
	}

	return u.String()
}

// EncodePathSegment encodes a single path segment (slugs, resource IDs,
// filenames, template names).
func EncodePathSegment(segment string) string {
	return url.PathEscape(segment)
}

// EncodeQueryValue encodes a single query parameter value (search terms,
// filter values, comma-joined lists passed as one param).
func EncodeQueryValue(value string) string {
	return url.QueryEscape(value)
}

// BuildQueryString builds an encoded query string from a map.
func BuildQueryString(params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values.Encode()
}
