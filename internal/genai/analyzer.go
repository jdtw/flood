// package genai implements a genai traffic camera analyzer.
package genai

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

// GenerativeModel is an interface for the Gemini model to facilitate testing.
type GenerativeModel interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

type TrafficAnalyzer struct {
	client       GenerativeModel
	model        string
	cameraURLs   []string
	lastCheck    time.Time
	cachedOpen   bool
	cachedDetail string
	mu           sync.Mutex
}

func NewTrafficAnalyzer(ctx context.Context, apiKey string, model string, cameraURLs []string) (*TrafficAnalyzer, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	return &TrafficAnalyzer{
		client:     client.Models,
		model:      model,
		cameraURLs: cameraURLs,
	}, nil
}

func (ta *TrafficAnalyzer) IsRoadOpen(ctx context.Context) (bool, string, time.Time, error) {
	if ta == nil || ta.client == nil {
		return false, "", time.Time{}, fmt.Errorf("AI client not initialized")
	}

	ta.mu.Lock()
	defer ta.mu.Unlock()

	if time.Since(ta.lastCheck) < 15*time.Minute && !ta.lastCheck.IsZero() {
		return ta.cachedOpen, ta.cachedDetail, ta.lastCheck, nil
	}

	parts := []*genai.Part{{Text: "Analyze these traffic camera images of the intersection of 124th " +
		"and SR203/Novelty Hill Rd. Determine if the road appears to be closed. " +
		"Look for 'Road Closed' signs, traffic cones, or barricades. Ignore normal " +
		"traffic. If the road is closed, reply with 'CLOSED: <reason>'. If the road " +
		"is open, reply with 'OPEN: <reason>'. Keep the reason short (1 sentence).",
	}}

	for _, url := range ta.cameraURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log.Printf("Error creating request for %s: %v", url, err)
			continue
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error fetching %s: %v", url, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("Error fetching %s: status %d", url, resp.StatusCode)
			resp.Body.Close() // Close the body even if status is not OK
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close() // Close the body immediately after reading
		if err != nil {
			log.Printf("Error reading body of %s: %v", url, err)
			continue
		}

		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: "image/jpeg",
				Data:     data,
			},
		})
	}

	if len(parts) <= 1 {
		return false, "", time.Time{}, fmt.Errorf("no images could be fetched")
	}

	content := []*genai.Content{{
		Parts: parts,
	}}

	resp, err := ta.client.GenerateContent(ctx, ta.model, content, nil)
	if err != nil {
		return false, "", time.Time{}, fmt.Errorf("gemini api error: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return false, "", time.Time{}, fmt.Errorf("empty response from gemini")
	}

	text := resp.Candidates[0].Content.Parts[0].Text
	text = strings.TrimSpace(text)

	detail := text
	isOpen := !strings.HasPrefix(strings.ToUpper(text), "CLOSED")

	if idx := strings.Index(detail, ":"); idx != -1 {
		detail = strings.TrimSpace(detail[idx+1:])
	}

	ta.cachedOpen = isOpen
	ta.cachedDetail = detail
	ta.lastCheck = time.Now()

	return isOpen, detail, ta.lastCheck, nil
}
