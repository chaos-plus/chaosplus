package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxUploadBytes = 20 << 20

func RegisterREST(mux *http.ServeMux, service *Service) {
	mux.HandleFunc("GET /api/work-items", func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListWorkItems(r.Context(), r.URL.Query().Get("type"), r.URL.Query().Get("status"), r.URL.Query().Get("parent"))
		respond(w, items, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/work-items", func(w http.ResponseWriter, r *http.Request) {
		var item WorkItem
		if err := decodeJSON(r, &item); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		created, err := service.CreateWorkItem(r.Context(), item)
		respond(w, created, err, http.StatusCreated)
	})
	mux.HandleFunc("GET /api/work-items/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := service.GetWorkItem(r.Context(), r.PathValue("id"))
		respond(w, item, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/work-items/{id}", func(w http.ResponseWriter, r *http.Request) {
		var patch WorkItemPatch
		if err := decodeJSON(r, &patch); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		item, err := service.UpdateWorkItem(r.Context(), r.PathValue("id"), patch)
		respond(w, item, err, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/work-items/{id}", func(w http.ResponseWriter, r *http.Request) {
		err := service.DeleteWorkItem(r.Context(), r.PathValue("id"))
		respond(w, map[string]bool{"ok": err == nil}, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/work-items/{id}/execute", func(w http.ResponseWriter, r *http.Request) {
		runID, err := service.ExecuteWorkItem(r.Context(), r.PathValue("id"))
		respond(w, map[string]string{"runId": runID}, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/work-items/{id}/attachments", func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListAttachments(r.Context(), "work_item", r.PathValue("id"))
		respond(w, items, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/work-items/{id}/attachments", uploadHandler(service, "work_item"))
	mux.HandleFunc("POST /api/channels/{id}/work-items", func(w http.ResponseWriter, r *http.Request) {
		var item WorkItem
		if err := decodeJSON(r, &item); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		created, err := service.CreateWorkItemFromChannel(r.Context(), r.PathValue("id"), item)
		respond(w, created, err, http.StatusCreated)
	})
	mux.HandleFunc("POST /api/channels/{id}/attachments", uploadHandler(service, "channel"))
	mux.HandleFunc("GET /api/channels/{id}/attachments", func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListAttachments(r.Context(), "channel", r.PathValue("id"))
		respond(w, items, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/attachments/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, err := service.GetAttachment(r.Context(), r.PathValue("id"))
		if err != nil {
			respond(w, nil, err, 0)
			return
		}
		mime, inline := SafeMime(a.Filename)
		disposition := "attachment"
		if inline {
			disposition = "inline"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disposition, url.PathEscape(a.Filename)))
		http.ServeFile(w, r, a.StorePath)
	})
	mux.HandleFunc("GET /api/okrs", func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListOkrs(r.Context())
		respond(w, items, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/okrs", func(w http.ResponseWriter, r *http.Request) {
		var okr Okr
		if err := decodeJSON(r, &okr); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		created, err := service.CreateOkr(r.Context(), okr)
		respond(w, created, err, http.StatusCreated)
	})
	mux.HandleFunc("PUT /api/okrs/{id}", func(w http.ResponseWriter, r *http.Request) {
		var okr Okr
		if err := decodeJSON(r, &okr); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		updated, err := service.UpdateOkr(r.Context(), r.PathValue("id"), okr)
		respond(w, updated, err, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/okrs/{id}", func(w http.ResponseWriter, r *http.Request) {
		err := service.DeleteOkr(r.Context(), r.PathValue("id"))
		respond(w, map[string]bool{"ok": err == nil}, err, http.StatusOK)
	})
}

func uploadHandler(service *Service, ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			respond(w, nil, ErrInvalid, 0)
			return
		}
		defer func() { _ = file.Close() }()
		a, err := service.UploadAttachment(r.Context(), ownerType, r.PathValue("id"), header.Filename, file)
		respond(w, a, err, http.StatusCreated)
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func respond(w http.ResponseWriter, body any, err error, success int) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrConflict):
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": strings.TrimSpace(err.Error())})
		return
	}
	w.WriteHeader(success)
	_ = json.NewEncoder(w).Encode(body)
}
