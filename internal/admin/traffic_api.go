package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"chijie/internal/traffic"
)

func (s *Server) loadTrafficConfig() (traffic.Config, error) {
	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg struct {
		Traffic traffic.Config `yaml:"traffic"`
	}
	if err := loadYAML(path, &cfg); err != nil {
		return traffic.DefaultConfig(), err
	}
	return traffic.NormalizeConfig(cfg.Traffic), nil
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, s.traffic.Snapshot(limit))
}

func (s *Server) handleTrafficGroupingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req traffic.URLNormalizationRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	cfg, rule, err := s.persistTrafficGroupingRule(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.traffic.UpdateConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule, "config": cfg})
}

func (s *Server) persistTrafficGroupingRule(rule traffic.URLNormalizationRule) (traffic.Config, traffic.URLNormalizationRule, error) {
	var storedRule traffic.URLNormalizationRule
	cfg, err := s.persistTrafficConfig(func(current traffic.Config) (traffic.Config, error) {
		var mergeErr error
		var next traffic.Config
		next, storedRule, mergeErr = traffic.MergeURLNormalizationRule(current, rule)
		return next, mergeErr
	})
	return cfg, storedRule, err
}

func (s *Server) handleTrafficSuccessFoldingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req traffic.SuccessFoldingRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	var storedRule traffic.SuccessFoldingRule
	cfg, err := s.persistTrafficConfig(func(current traffic.Config) (traffic.Config, error) {
		var mergeErr error
		var next traffic.Config
		next, storedRule, mergeErr = traffic.MergeSuccessFoldingRule(current, req)
		return next, mergeErr
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.traffic.UpdateConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]any{"rule": storedRule, "config": cfg})
}

func (s *Server) handleTrafficSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, http.StatusOK, s.traffic.Config().Persistence)
	case "PUT":
		var req traffic.PersistenceConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		if req.RetentionDays != 0 && (req.RetentionDays < 1 || req.RetentionDays > traffic.MaxRetentionDays) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("retention_days must be between 1 and %d", traffic.MaxRetentionDays)})
			return
		}
		cfg, err := s.persistTrafficConfig(func(current traffic.Config) (traffic.Config, error) {
			current = traffic.NormalizeConfig(current)
			if req.Enabled != nil {
				current.Persistence.Enabled = req.Enabled
			}
			if req.RetentionDays != 0 {
				current.Persistence.RetentionDays = req.RetentionDays
			}
			return traffic.NormalizeConfig(current), nil
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.traffic.UpdateConfig(cfg)
		writeJSON(w, http.StatusOK, cfg.Persistence)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) persistTrafficConfig(update func(traffic.Config) (traffic.Config, error)) (traffic.Config, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	path := filepath.Join(s.configDir, "gateway.yaml")
	var raw map[string]any
	if err := loadYAML(path, &raw); err != nil {
		return traffic.Config{}, err
	}
	var typed struct {
		Traffic traffic.Config `yaml:"traffic"`
	}
	if err := loadYAML(path, &typed); err != nil {
		return traffic.Config{}, err
	}
	cfg, err := update(typed.Traffic)
	if err != nil {
		return traffic.Config{}, err
	}
	cfg = traffic.NormalizeConfig(cfg)
	if raw == nil {
		raw = map[string]any{}
	}
	raw["traffic"] = cfg
	if err := atomicWriteYAML(path, raw); err != nil {
		return traffic.Config{}, err
	}
	return cfg, nil
}
