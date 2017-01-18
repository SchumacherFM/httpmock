package httpmock_test

import (
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
)

var testUrl = "http://www.example.com/"

func TestMockTransport(t *testing.T) {
	t.Parallel()

	httpmock.Activate()
	defer httpmock.Deactivate()

	url := "https://github.com/"
	body := "hello world"

	httpmock.RegisterResponder("GET", url, httpmock.NewStringResponder(200, body))

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != body {
		t.FailNow()
	}

	// the http client wraps our NoResponderFound error, so we just try and match on text
	if _, err := http.Get(testUrl); !strings.Contains(err.Error(),
		httpmock.NoResponderFound.Error()) {

		t.Fatal(err)
	}
}

func TestMockTransportReset(t *testing.T) {
	t.Parallel()

	httpmock.DeactivateAndReset()

	if httpmock.Transports.Default.Len() > 0 {
		t.Fatal("expected no responders at this point")
	}

	httpmock.RegisterResponder("GET", testUrl, nil)

	if httpmock.Transports.Default.Len() != 1 {
		t.Fatal("expected one responder")
	}

	httpmock.Reset()

	if httpmock.Transports.Default.Len() > 0 {
		t.Fatal("expected no responders as they were just reset")
	}
}

func TestMockTransportNoResponder(t *testing.T) {
	t.Parallel()

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	httpmock.Reset()

	if !httpmock.Transports.Default.HasNoResponder() {
		t.Fatal("expected noResponder to be nil")
	}

	if _, err := http.Get(testUrl); err == nil {
		t.Fatal("expected to receive a connection error due to lack of responders")
	}

	httpmock.RegisterNoResponder(httpmock.NewStringResponder(200, "hello world"))

	resp, err := http.Get(testUrl)
	if err != nil {
		t.Fatal("expected request to succeed")
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "hello world" {
		t.Fatal("expected body to be 'hello world'")
	}
}

func TestMockTransportQuerystringFallback(t *testing.T) {
	t.Parallel()

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	// register the testUrl responder
	httpmock.RegisterResponder("GET", testUrl, httpmock.NewStringResponder(200, "hello world"))

	// make a request for the testUrl with a querystring
	resp, err := http.Get(testUrl + "?hello=world")
	if err != nil {
		t.Fatal("expected request to succeed")
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "hello world" {
		t.Fatal("expected body to be 'hello world'")
	}
}

type dummyTripper struct{}

func (d *dummyTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestMockTransportInitialTransport(t *testing.T) {
	t.Parallel()

	httpmock.DeactivateAndReset()

	tripper := &dummyTripper{}
	http.DefaultTransport = tripper

	httpmock.Activate()

	if http.DefaultTransport == tripper {
		t.Fatal("expected http.DefaultTransport to be a mock transport")
	}

	httpmock.Deactivate()

	if http.DefaultTransport != tripper {
		t.Fatal("expected http.DefaultTransport to be dummy")
	}
}

func TestMockTransportNonDefault(t *testing.T) {
	t.Parallel()

	// create a custom http client w/ custom Roundtripper
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   60 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 60 * time.Second,
		},
	}

	// activate mocks for the client
	httpmock.ActivateNonDefault(client)
	defer httpmock.DeactivateAndReset()

	body := "hello world!"

	httpmock.RegisterResponder("GET", testUrl, httpmock.NewStringResponder(200, body))

	req, err := http.NewRequest("GET", testUrl, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != body {
		t.FailNow()
	}
}
