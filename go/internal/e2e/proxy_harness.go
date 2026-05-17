// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const fixtureProviderOutputTokens = 3

type ProxyProviderServer struct {
	server *httptest.Server
}

func NewProxyProviderServer(t *testing.T) *ProxyProviderServer {
	t.Helper()

	provider := &ProxyProviderServer{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(provider.server.Close)

	return provider
}

func (provider *ProxyProviderServer) URL() string {
	return provider.server.URL
}

func (provider *ProxyProviderServer) Send(
	request agentproxy.ProviderRequest,
) (agentproxy.ProviderResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("encode provider request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		provider.URL(),
		bytes.NewReader(payload),
	)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("create provider request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("send provider request: %w", err)
	}
	defer response.Body.Close()

	var decoded agentproxy.ProviderResponse

	err = json.NewDecoder(response.Body).Decode(&decoded)
	if err != nil {
		return agentproxy.ProviderResponse{}, fmt.Errorf("decode provider response: %w", err)
	}

	return decoded, nil
}

func (provider *ProxyProviderServer) handle(
	writer http.ResponseWriter,
	request *http.Request,
) {
	defer request.Body.Close()

	var decoded agentproxy.ProviderRequest

	err := json.NewDecoder(request.Body).Decode(&decoded)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)

		return
	}

	response := agentproxy.ProviderResponse{
		SessionID: decoded.SessionID,
		Provider:  decoded.Provider,
		Model:     decoded.Model,
		Messages: []agentproxy.Message{{
			Role:    agentproxy.RoleAssistant,
			Content: "fixture provider response",
		}},
		Usage: agentproxy.TokenUsage{
			InputTokens:  len(decoded.Messages),
			OutputTokens: fixtureProviderOutputTokens,
			TotalTokens: len(decoded.Messages) +
				fixtureProviderOutputTokens,
		},
	}

	writer.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(writer).Encode(response)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}
