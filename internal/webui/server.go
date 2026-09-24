// Package webui serves the review UI: waveform, property automaton, active
// threads and minimal counterexample, all with stable identities and sampling
// regions, plus the JSON API used by the page and the end-to-end tests.
package webui

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"gsb/internal/cases"
	"gsb/internal/domain"
	"gsb/internal/service"
	"gsb/internal/store"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	svc *service.Service
	db  *store.DB
	mux *http.ServeMux
}

func New(db *store.DB) *Server {
	s := &Server{svc: service.New(db), db: db, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	s.mux.HandleFunc("GET /", s.index)
	s.mux.HandleFunc("GET /api/state", s.apiState)
	s.mux.HandleFunc("GET /api/narrative", s.apiNarrative)
	s.mux.HandleFunc("POST /api/narrative/next", s.apiNarrativeNext)
	s.mux.HandleFunc("POST /api/replay", s.apiReplay)
	s.mux.HandleFunc("POST /api/adjudicate", s.apiAdjudicate)
	s.mux.HandleFunc("POST /api/undo", s.apiUndo)
	s.mux.HandleFunc("POST /api/branch/semver", s.apiBranchSemver)
	s.mux.HandleFunc("POST /api/branch/source", s.apiBranchSource)
	s.mux.HandleFunc("GET /api/export", s.apiExport)
	s.mux.HandleFunc("POST /api/reimport", s.apiReimport)
	s.mux.HandleFunc("GET /api/history", s.apiHistory)
	s.mux.HandleFunc("GET /wave.svg", s.waveSVG)
	s.mux.HandleFunc("GET /automaton.svg", s.automatonSVG)
	s.mux.HandleFunc("GET /counterex.svg", s.counterexSVG)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := staticFS.ReadFile("static/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) narrativeState(caseID string) (*service.NarrativeState, error) {
	raw, err := s.db.GetMeta("narrative:" + caseID)
	if err != nil {
		return nil, err
	}
	st := &service.NarrativeState{CaseID: caseID}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), st)
	}
	return st, nil
}

func (s *Server) saveNarrative(st *service.NarrativeState) {
	b, _ := json.Marshal(st)
	_ = s.db.SetMeta("narrative:"+st.CaseID, string(b))
}

// apiState returns the full review state for one branch.
func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	caseID := r.URL.Query().Get("case")
	if caseID == "" {
		id, err := s.svc.EnsureCase()
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		caseID = id
	}
	branchID := r.URL.Query().Get("branch")
	branches, err := s.db.ListBranches(caseID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if branchID == "" {
		for _, b := range branches {
			if b.Current {
				branchID = b.ID
			}
		}
		if branchID == "" && len(branches) > 0 {
			branchID = branches[0].ID
		}
	}
	data, err := s.db.LoadCaseData(caseID, branchID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	verdicts, _ := s.db.LoadVerdicts(caseID, branchID)
	adjs, _ := s.db.ListAdjudications(caseID, branchID)
	events, _ := s.db.Events(caseID, branchID)
	ver, _ := s.db.BranchVersion(caseID, branchID)
	narr, _ := cases.LoadNarrative()
	nstate, _ := s.narrativeState(caseID)

	var cexes []*store.StoredCounterex
	for _, v := range verdicts {
		if cx, _ := s.db.LoadCounterex(caseID, branchID, v.ID); cx != nil {
			cexes = append(cexes, cx)
		}
	}
	writeJSON(w, 200, map[string]any{
		"case": map[string]any{
			"id": caseID, "name": data.Name, "timescale": data.Timescale,
			"signals": data.Signals, "domains": data.Domains,
			"diagnostics": data.Diagnostics, "change_count": len(data.Changes),
		},
		"branch": branchID, "branch_version": ver, "branches": branches,
		"properties": data.Properties, "verdicts": verdicts,
		"counterexamples": cexes, "adjudications": adjs, "events": events,
		"narrative": narr, "narrative_state": nstate,
	})
}

func (s *Server) apiNarrative(w http.ResponseWriter, r *http.Request) {
	narr, err := cases.LoadNarrative()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, narr)
}

func (s *Server) apiNarrativeNext(w http.ResponseWriter, r *http.Request) {
	caseID, err := s.svc.EnsureCase()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	st, err := s.narrativeState(caseID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	narr, _ := cases.LoadNarrative()
	if st.Step >= len(narr.Steps) {
		writeJSON(w, 200, map[string]any{"done": true, "state": st})
		return
	}
	step := narr.Steps[st.Step]
	st, err = s.svc.RunStep(st, step.Action)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.saveNarrative(st)
	writeJSON(w, 200, map[string]any{"done": false, "executed": step, "state": st})
}

func (s *Server) apiReplay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Case   string `json:"case"`
		Branch string `json:"branch"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Actor == "" {
		req.Actor = "ui"
	}
	sums, err := s.svc.RunReplay(req.Case, req.Branch, req.Actor)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"verdicts": sums})
}

func (s *Server) apiAdjudicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Case     string `json:"case"`
		Branch   string `json:"branch"`
		Target   string `json:"target"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Actor    string `json:"actor"`
		Version  int64  `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	adj, conflict, err := s.db.Adjudicate(req.Case, req.Branch, req.Target,
		req.Decision, req.Reason, req.Actor, req.Version)
	if errors.Is(err, store.ErrConflict) {
		writeJSON(w, 409, map[string]any{"error": err.Error(), "conflict": conflict})
		return
	}
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, adj)
}

func (s *Server) apiUndo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Case         string `json:"case"`
		Branch       string `json:"branch"`
		Adjudication string `json:"adjudication"`
		Actor        string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	ver, err := s.db.UndoAdjudication(req.Case, req.Branch, req.Adjudication, req.Actor)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"version": ver})
}

func (s *Server) apiBranchSemver(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Case   string `json:"case"`
		Parent string `json:"parent"`
		SemVer string `json:"semver"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	id, err := s.svc.ReviseSemVer(req.Case, req.Parent, req.SemVer, req.Actor)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"branch": id})
}

func (s *Server) apiBranchSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Case   string `json:"case"`
		Parent string `json:"parent"`
		Kind   string `json:"kind"`
		Note   string `json:"note"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Kind != "vcd" {
		writeErr(w, 400, errors.New("only vcd source changes are bundled in the case pack"))
		return
	}
	id, err := s.svc.SourceChange(req.Case, req.Parent, "vcd", cases.Wave2VCD, req.Actor, req.Note)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"branch": id})
}

func (s *Server) apiExport(w http.ResponseWriter, r *http.Request) {
	caseID := r.URL.Query().Get("case")
	b, err := s.svc.ExportJSON(caseID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=case-bundle.json")
	w.Write(b)
}

// apiReimport deletes the local case and re-imports from the exported bundle,
// proving identity/order/provenance stability.
func (s *Server) apiReimport(w http.ResponseWriter, r *http.Request) {
	var bundle store.CaseBundle
	if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.DeleteCase(bundle.CaseID); err != nil {
		writeErr(w, 500, err)
		return
	}
	id, err := s.db.ImportCase(bundle.Name,
		bundle.Sources["vcd"].Raw, bundle.Sources["clockdomains"].Raw, bundle.Sources["properties"].Raw)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"case": id, "expected": bundle.CaseID})
}

func (s *Server) apiHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seq, _ := strconv.ParseInt(q.Get("seq"), 10, 64)
	snap, err := SnapshotAt(s.db, q.Get("case"), q.Get("branch"), seq)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, snap)
}

var _ = domain.TriTrue
