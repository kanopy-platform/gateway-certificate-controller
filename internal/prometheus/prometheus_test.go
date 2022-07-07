package prometheus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()

	Handler().ServeHTTP(rr, req)
	body := rr.Body.String()
	assert.Contains(t, body, `managed_certificates_count`)
}
