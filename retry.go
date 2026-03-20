package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// retryRequest performs an HTTP request with configurable retries and backoffs.
// backoffs defines the sleep duration before each retry attempt.
// E.g., []time.Duration{1*time.Second} = 1 retry after 1s.
//
//	[]time.Duration{1*time.Second, 3*time.Second} = 2 retries after 1s and 3s.
func retryRequest(ctx context.Context, client *http.Client, req *http.Request, backoffs []time.Duration) (*http.Response, error) {
	// Attach context to request
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err == nil && resp.StatusCode < 400 {
		return resp, nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Retry loop
	for i, backoff := range backoffs {
		log.Printf("[RETRY] Attempt %d/%d for %s", i+1, len(backoffs), req.URL)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		retryReq := req.Clone(ctx)
		resp, err = client.Do(retryReq)
		if err == nil && resp.StatusCode < 400 {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", len(backoffs), err)
	}
	return nil, fmt.Errorf("HTTP %d after %d retries", resp.StatusCode, len(backoffs))
}
