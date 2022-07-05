package prometheus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()

	Handler().ServeHTTP(rr, req)
	body := rr.Body.String()
	assert.Contains(t, body, `mutation_webhook_duration_seconds`)
	assert.Contains(t, body, `managed_certificates_count`)
}
