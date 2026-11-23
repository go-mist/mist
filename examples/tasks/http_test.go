package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-mist/mist/query"
)

type memStore struct {
	next  int64
	tasks []Task
}

func (m *memStore) Add(_ context.Context, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errTitle
	}
	m.next++
	task := Task{ID: m.next, Title: title}
	m.tasks = append(m.tasks, task)
	return task, nil
}

func (m *memStore) List(context.Context) ([]Task, error) {
	return append([]Task(nil), m.tasks...), nil
}

func (m *memStore) Done(_ context.Context, id int64) error {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks[i].Done = true
			return nil
		}
	}
	return query.ErrNoRows
}

func (m *memStore) Delete(_ context.Context, id int64) error {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return nil
		}
	}
	return query.ErrNoRows
}

func TestHTTPTasks(t *testing.T) {
	srv := httptest.NewServer(newMux(&memStore{}))
	defer srv.Close()

	res := request(t, http.MethodPost, srv.URL+"/tasks", `{"title":"buy milk"}`)
	if res.StatusCode != http.StatusCreated || !strings.Contains(res.Body, `"id":1`) || !strings.Contains(res.Body, `"title":"buy milk"`) {
		t.Fatalf("create: %d %s", res.StatusCode, res.Body)
	}

	res = request(t, http.MethodGet, srv.URL+"/tasks", "")
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Body, "buy milk") {
		t.Fatalf("list: %d %s", res.StatusCode, res.Body)
	}

	res = request(t, http.MethodPost, srv.URL+"/tasks/1/done", "")
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("done: %d %s", res.StatusCode, res.Body)
	}
	res = request(t, http.MethodGet, srv.URL+"/tasks", "")
	if !strings.Contains(res.Body, `"done":true`) {
		t.Fatalf("done did not stick: %s", res.Body)
	}

	res = request(t, http.MethodDelete, srv.URL+"/tasks/1", "")
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, res.Body)
	}
	res = request(t, http.MethodGet, srv.URL+"/tasks", "")
	if strings.Contains(res.Body, "buy milk") {
		t.Fatalf("delete left the task: %s", res.Body)
	}

	res = request(t, http.MethodPost, srv.URL+"/tasks", `{"title":"  "}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty title: %d %s", res.StatusCode, res.Body)
	}
	res = request(t, http.MethodDelete, srv.URL+"/tasks/9", "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing: %d %s", res.StatusCode, res.Body)
	}
	res = request(t, http.MethodPost, srv.URL+"/tasks/nope/done", "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", res.StatusCode, res.Body)
	}
}

func request(t *testing.T, method, url, body string) httpResult {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{StatusCode: res.StatusCode, Body: string(bytes.TrimSpace(raw))}
}

type httpResult struct {
	StatusCode int
	Body       string
}

func TestReadJSONRejectsTrailing(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"title":"a"}{"title":"b"}`))
	if err := readJSON(req, &struct{}{}); err == nil || err.Error() != "bad json" {
		t.Fatal(err)
	}
}
