package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type analyzeRequest struct {
	Grammar string   `json:"grammar"`
	Start   string   `json:"start"`
	Tokens  []string `json:"tokens"`
	Input   string   `json:"input"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "仅支持 POST"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "请求体不是合法 JSON：" + err.Error()})
		return
	}

	tokens := req.Tokens
	if len(tokens) == 0 {
		tokens = strings.Fields(req.Input) // 兼容直接提交空白分隔的输入串
	}

	result, err := analyze(req.Grammar, req.Start, tokens)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/analyze", handleAnalyze)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("grammar 服务监听 :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
