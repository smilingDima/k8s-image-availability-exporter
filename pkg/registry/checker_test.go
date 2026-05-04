package registry

import (
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/flant/k8s-image-availability-exporter/pkg/store"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/sirupsen/logrus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseImageName(t *testing.T) {
	const (
		goodImageName                = "docker.io/test:test"
		goodImageNameWithoutRegistry = "test:test"
		badImageName                 = "te*^#@@st"

		defaultRegistryName = "test-registry.io"
	)

	_, err := parseImageName(goodImageName, "", false)
	require.NoError(t, err)

	_, err = parseImageName(badImageName, "", false)
	require.Error(t, err)

	ref, err := parseImageName(goodImageNameWithoutRegistry, defaultRegistryName, false)
	require.NoError(t, err)
	require.Equal(t, path.Join(defaultRegistryName, goodImageNameWithoutRegistry), ref.Name())
}

var log = logrus.NewEntry(logrus.StandardLogger())
var kcMock authn.Keychain = nil

// Return statusCode from first 3 chars of http host.
// For example: request to 404.docker.io returns statusCode 404 (NotFound)
type MockRegistryTransportStatuses struct {
}

func (m *MockRegistryTransportStatuses) RoundTrip(req *http.Request) (*http.Response, error) {
	statusCode, err := strconv.Atoi(req.Host[:3])
	if err == nil {
		return &http.Response{
			StatusCode: statusCode,
			Body:       http.NoBody,
			Header: http.Header{
				"Content-Type":          {"application/vnd.docker.distribution.manifest.v1+json"},
				"Docker-Content-Digest": {"sha256:33e0bbc7ca9ecf108140af6288c7c9d1ecc77548cbfd3952fd8466a75edefe57"},
			},
		}, nil
	} else {
		return &http.Response{StatusCode: http.StatusRequestTimeout, Body: http.NoBody}, nil
	}
}

func Test_checkImageAvailability_statuses(t *testing.T) {
	rc := &Checker{
		config: registryCheckerConfig{
			defaultRegistry: "index.docker.io",
			mirrorsMap:      nil,
			plainHTTP:       false,
		},
		registryTransport: &MockRegistryTransportStatuses{},
	}

	mode := rc.checkImageAvailability(log, fmt.Sprintf("%d.local/test:test", http.StatusOK), kcMock)
	assert.Equal(t, store.Available, mode)

	mode = rc.checkImageAvailability(log, fmt.Sprintf("%d.local/test:test", http.StatusNotFound), kcMock)
	assert.Equal(t, store.Absent, mode)

	mode = rc.checkImageAvailability(log, fmt.Sprintf("%d.local/test:test", http.StatusUnauthorized), kcMock)
	assert.Equal(t, store.AuthnFailure, mode)

	mode = rc.checkImageAvailability(log, fmt.Sprintf("%d.local/test:test", http.StatusForbidden), kcMock)
	assert.Equal(t, store.AuthzFailure, mode)

	mode = rc.checkImageAvailability(log, fmt.Sprintf("%d.local/test:test", http.StatusRequestTimeout), kcMock)
	assert.Equal(t, store.UnknownError, mode)
}

// It is assumed that defaultRegistry is unavailable, but mirrors are working.
type MockMirrorRegistryTransport struct {
}

func (m *MockMirrorRegistryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.Host, "mirror") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header: http.Header{
				"Content-Type":          {"application/vnd.docker.distribution.manifest.v1+json"},
				"Docker-Content-Digest": {"sha256:33e0bbc7ca9ecf108140af6288c7c9d1ecc77548cbfd3952fd8466a75edefe57"},
			},
		}, nil
	} else {
		return &http.Response{
			StatusCode: http.StatusRequestTimeout,
			Body:       http.NoBody,
		}, nil
	}
}

func Test_checkImageAvailability_mirrorForDefaultRegistry(t *testing.T) {
	rc := &Checker{
		config: registryCheckerConfig{
			defaultRegistry: "index.docker.io",
			mirrorsMap:      map[string]string{"index.docker.io": "mirror.local"},
			plainHTTP:       false,
		},
		registryTransport: &MockMirrorRegistryTransport{},
	}

	mode := rc.checkImageAvailability(log, "test:test", kcMock)
	assert.Equal(t, store.Available, mode)

	mode = rc.checkImageAvailability(log, "index.docker.io/test:test", kcMock)
	assert.Equal(t, store.Available, mode)

	mode = rc.checkImageAvailability(log, "docker.io/test:test", kcMock)
	assert.Equal(t, store.Available, mode)

	mode = rc.checkImageAvailability(log, "local/test:test", kcMock)
	assert.Equal(t, store.Available, mode)
}
