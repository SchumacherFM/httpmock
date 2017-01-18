package httpmock

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// Responders are callbacks that receive and http request and return a mocked
// response.
type Responder func(*http.Request) (*http.Response, error)

// NoResponderFound is returned when no responders are found for a given HTTP
// method and URL.
var NoResponderFound = errors.New("no responder found")

// ConnectionFailure is a responder that returns a connection failure.  This is
// the default responder, and is called when no other matching responder is
// found.
func ConnectionFailure(*http.Request) (*http.Response, error) {
	return nil, NoResponderFound
}

// NewMockTransport creates a new *MockTransport with no responders.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		responders: make(map[string]Responder),
	}
}

// MockTransport implements http.RoundTripper, which fulfills single http
// requests issued by an http.Client.  This implementation doesn't actually make
// the call, instead deferring to the registered list of responders.
type MockTransport struct {
	mu          sync.Mutex
	responders  map[string]Responder
	noResponder Responder
}

// RoundTrip receives HTTP requests and routes them to the appropriate
// responder.  It is required to implement the http.RoundTripper interface.  You
// will not interact with this directly, instead the *http.Client you are using
// will call it for you.
func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()

	// try and get a responder that matches the method and URL
	responder := m.responderForKey(req.Method + " " + url)

	// if we weren't able to find a responder and the URL contains a querystring
	// then we strip off the querystring and try again.
	if responder == nil && strings.Contains(url, "?") {
		responder = m.responderForKey(req.Method + " " + strings.Split(url, "?")[0])
	}

	// if we found a responder, call it
	if responder != nil {
		return responder(req)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// we didn't find a responder, so fire the 'no responder' responder
	if m.noResponder == nil {
		return ConnectionFailure(req)
	}
	return m.noResponder(req)
}

// do nothing with timeout
func (m *MockTransport) CancelRequest(req *http.Request) {}

// responderForKey returns a responder for a given key
func (m *MockTransport) responderForKey(key string) Responder {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.responders {
		if k != key {
			continue
		}
		return r
	}
	return nil
}

// RegisterResponder adds a new responder, associated with a given HTTP method
// and URL.  When a request comes in that matches, the responder will be called
// and the response returned to the client.
func (m *MockTransport) RegisterResponder(method, url string, responder Responder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responders[method+" "+url] = responder
}

// RegisterNoResponder is used to register a responder that will be called if no
// other responder is found.  The default is ConnectionFailure.
func (m *MockTransport) RegisterNoResponder(responder Responder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.noResponder = responder
}

// Reset removes all registered responders (including the no responder) from the
// MockTransport
func (m *MockTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responders = make(map[string]Responder)
	m.noResponder = nil
}

// Len returns the current number for registered responders.
func (m *MockTransport) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.responders)
}

// HasNoResponder returns true if there is a registered NoResponder.
func (m *MockTransport) HasNoResponder() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.noResponder != nil
}

// Transports defines the default mock transport and the default net/http
// transport.
var Transports = &struct {
	sync.Mutex
	// Default is the default mock transport used by Activate,
	// Deactivate, Reset, DeactivateAndReset, RegisterResponder, and
	// RegisterNoResponder.
	Default *MockTransport
	// Initial is a cache of the original transport used so we can put
	// it back when Deactivate is called.
	Initial http.RoundTripper
}{
	Default: NewMockTransport(),
	Initial: http.DefaultTransport,
}

// Used to handle custom http clients (i.e clients other than
// http.DefaultClient)
var oldHTTPGlobals = &struct {
	sync.Mutex
	transport http.RoundTripper
	client    *http.Client
}{}

var queue = &struct {
	wait     chan bool
	isLocked *int32
}{
	wait:     make(chan bool),
	isLocked: new(int32),
}

// Activate starts the mock environment.  This should be called before your
// tests run.  Under the hood this replaces the Transport on the
// http.DefaultClient with DefaultTransport.
//
// To enable mocks for a test, simply activate at the beginning of a test:
// 		func TestFetchArticles(t *testing.T) {
// 			httpmock.Activate()
// 			// all http requests will now be intercepted
// 		}
//
// If you want all of your tests in a package to be mocked, just call Activate
// from init():
// 		func init() {
// 			httpmock.Activate()
// 		}
func Activate() {
	if Disabled() {
		return
	}

	if atomic.LoadInt32(queue.isLocked) >= 1 {
		atomic.StoreInt32(queue.isLocked, 2)
		println("Waiting")
		select {
		case <-queue.wait:
		}
	} else {
		atomic.StoreInt32(queue.isLocked, 1)
	}

	// make sure that if Activate is called multiple times it doesn't overwrite
	// the InitialTransport with a mock transport.
	Transports.Lock()
	defer Transports.Unlock()

	if http.DefaultTransport != Transports.Default {
		Transports.Initial = http.DefaultTransport
	}

	http.DefaultTransport = Transports.Default
}

// ActivateNonDefault starts the mock environment with a non-default
// http.Client. This emulates the Activate function, but allows for custom
// clients that do not use http.DefaultTransport
//
// To enable mocks for a test using a custom client, activate at the beginning
// of a test:
// 		client := &http.Client{Transport: &http.Transport{TLSHandshakeTimeout: 60 * time.Second}}
// 		httpmock.ActivateNonDefault(client)
func ActivateNonDefault(client *http.Client) {
	if Disabled() {
		return
	}
	if atomic.LoadInt32(queue.isLocked) >= 1 {
		atomic.StoreInt32(queue.isLocked, 2)
		select {
		case <-queue.wait:
		}
	} else {
		atomic.StoreInt32(queue.isLocked, 1)
	}

	oldHTTPGlobals.Lock()
	defer oldHTTPGlobals.Unlock()
	Transports.Lock()
	defer Transports.Unlock()

	// save the custom client & it's RoundTripper
	oldHTTPGlobals.transport = client.Transport
	oldHTTPGlobals.client = client
	client.Transport = Transports.Default
}

// Deactivate shuts down the mock environment.  Any HTTP calls made after this
// will use a live transport.
//
// Usually you'll call it in a defer right after activating the mock environment:
// 		func TestFetchArticles(t *testing.T) {
// 			httpmock.Activate()
// 			defer httpmock.Deactivate()
//
// 			// when this test ends, the mock environment will close
// 		}
func Deactivate() {
	if Disabled() {
		return
	}
	if isL := atomic.LoadInt32(queue.isLocked); isL >= 1 {
		atomic.StoreInt32(queue.isLocked, 0)
		if isL == 2 {
			queue.wait <- true
		}
	}

	oldHTTPGlobals.Lock()
	defer oldHTTPGlobals.Unlock()
	Transports.Lock()
	defer Transports.Unlock()
	http.DefaultTransport = Transports.Initial

	// reset the custom client to use it's original RoundTripper
	if oldHTTPGlobals.client != nil {
		oldHTTPGlobals.client.Transport = oldHTTPGlobals.transport
	}
}

// Reset will remove any registered mocks and return the mock environment to
// it's initial state.
func Reset() {
	Transports.Lock()
	defer Transports.Unlock()
	Transports.Default.Reset()
}

// DeactivateAndReset is just a convenience method for calling Deactivate() and
// then Reset() Happy deferring!
func DeactivateAndReset() {
	Deactivate()
	Reset()
}

// RegisterResponder adds a mock that will catch requests to the given HTTP
// method and URL, then route them to the Responder which will generate a
// response to be returned to the client.
//
// Example:
// 		func TestFetchArticles(t *testing.T) {
// 			httpmock.Activate()
// 			httpmock.DeactivateAndReset()
//
// 			httpmock.RegisterResponder("GET", "http://example.com/",
// 				httpmock.NewStringResponder("hello world", 200))
//
//			// requests to http://example.com/ will now return 'hello world'
// 		}
func RegisterResponder(method, url string, responder Responder) {
	Transports.Lock()
	defer Transports.Unlock()
	Transports.Default.RegisterResponder(method, url, responder)
}

// RegisterNoResponder adds a mock that will be called whenever a request for an
// unregistered URL is received.  The default behavior is to return a connection
// error.
//
// In some cases you may not want all URLs to be mocked, in which case you can do this:
// 		func TestFetchArticles(t *testing.T) {
// 			httpmock.Activate()
// 			httpmock.DeactivateAndReset()
//			httpmock.RegisterNoResponder(httpmock.InitialTransport.RoundTrip)
//
// 			// any requests that don't have a registered URL will be fetched normally
// 		}
func RegisterNoResponder(responder Responder) {
	Transports.Lock()
	defer Transports.Unlock()
	Transports.Default.RegisterNoResponder(responder)
}
