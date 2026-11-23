package main

import (
	"github.com/gabeduke/hindsight/internal/analyze"
	"github.com/gabeduke/hindsight/internal/config"
	"github.com/gabeduke/hindsight/internal/similar"
)

// analyzers is every analysis this build can run on a take, in the order a
// pass tries them (internal/analyze). Each is one package behind
// analyze.Analyzer; one that needs configuration (a transcription service's
// address) is left out when it has none, so a Pi that was never set up for it
// runs nothing and shows no button for it.
func analyzers(cfg *config.Config) []analyze.Analyzer {
	var as []analyze.Analyzer
	if cfg.AnalyzeHarmony {
		as = append(as, similar.Analyzer{})
	}
	return as
}
