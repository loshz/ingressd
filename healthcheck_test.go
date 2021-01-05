package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
)

type mockDoer struct {
	doFunc func(*http.Request) (*http.Response, error)
	err    bool
}

func (m mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestEnsureHostHealthChecks(t *testing.T) {
	t.Parallel()

	testTable := make(map[string]mockDoer)

	testTable["TestHTTPDoError"] = mockDoer{
		doFunc: func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("http do error")
		},
		err: true,
	}

	testTable["TestInvalidStatusCode"] = mockDoer{
		doFunc: func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       ioutil.NopCloser(nil),
				Status:     "bad request",
				StatusCode: http.StatusBadRequest,
			}, nil
		},
		err: true,
	}

	testTable["TestSuccess"] = mockDoer{
		doFunc: func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       ioutil.NopCloser(nil),
				StatusCode: http.StatusOK,
			}, nil
		},
		err: false,
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			err := ensureHostHealthChecks(test, net.ParseIP("192.168.0.1"), "syscll.org")
			if test.err && err == nil {
				t.Errorf("expected error, got: nil")
			}
			if !test.err && err != nil {
				t.Errorf("expected error: nil, got: %v", err)
			}
		})
	}
}
