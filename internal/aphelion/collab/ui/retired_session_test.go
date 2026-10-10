package ui

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRetiredSessionErrorShowsServiceExplanation(t *testing.T) {
	response := func(status int, body string) *http.Response {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
	}
	err := retiredSessionError(response(http.StatusGone, `{"code":"retired_protocol","message":"Session s was hosted with collaboration protocol v1."}`))
	if err == nil || err.Error() != "Session s was hosted with collaboration protocol v1." {
		t.Fatalf("retired session error = %v", err)
	}
	for _, other := range []*http.Response{
		response(http.StatusNotFound, `{"code":"retired_protocol","message":"x"}`),
		response(http.StatusGone, `{"code":"other","message":"x"}`),
		response(http.StatusGone, `not json`),
	} {
		if err := retiredSessionError(other); err != nil {
			t.Fatalf("unrelated response produced %v", err)
		}
	}
	long := retiredSessionError(response(http.StatusGone, `{"code":"retired_protocol","message":"`+strings.Repeat("x", 2000)+`"}`))
	if long == nil || len(long.Error()) != 512 {
		t.Fatal("service message was not bounded")
	}
}
