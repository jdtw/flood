package genai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"
)

type mockGenerativeModel struct {
	response *genai.GenerateContentResponse
	err      error
}

func (m *mockGenerativeModel) GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	return m.response, m.err
}

func TestTrafficAnalyzer_IsRoadOpen(t *testing.T) {
	// Start a local test server to serve dummy images
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake image data"))
	}))
	defer ts.Close()

	cameraURLs := []string{ts.URL + "/cam1.jpg"}

	tests := []struct {
		name           string
		mockResponse   *genai.GenerateContentResponse
		mockError      error
		expectedOpen   bool
		expectedDetail string
		expectError    bool
	}{
		{
			name: "Road Closed",
			mockResponse: &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{
								{Text: "CLOSED: The road is completely flooded."},
							},
						},
					},
				},
			},
			expectedOpen:   false,
			expectedDetail: "The road is completely flooded.",
		},
		{
			name: "Road Open",
			mockResponse: &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{
								{Text: "OPEN: Traffic is moving normally."},
							},
						},
					},
				},
			},
			expectedOpen:   true,
			expectedDetail: "Traffic is moving normally.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ta := &TrafficAnalyzer{
				client: &mockGenerativeModel{
					response: tt.mockResponse,
					err:      tt.mockError,
				},
				cameraURLs: cameraURLs,
			}

			open, detail, _, err := ta.IsRoadOpen(context.Background())

			if (err != nil) != tt.expectError {
				t.Errorf("IsRoadOpen() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if open != tt.expectedOpen {
				t.Errorf("IsRoadOpen() open = %v, want %v", open, tt.expectedOpen)
			}
			if !strings.Contains(detail, tt.expectedDetail) {
				t.Errorf("IsRoadOpen() detail = %q, want to contain %q", detail, tt.expectedDetail)
			}
		})
	}
}
