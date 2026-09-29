package report

import (
	"fmt"
	"sort"

	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/diff"
)

// patch computes patch coverage: of the lines this diff added, how many can
// a layer cover, and how many did it. Per layer, because the gate is per
// layer ("80% of the changed coverable lines in each layer that has at least
// min_lines of them"), plus an informational union across the gated layers
// for a headline number.
func (a *analyzer) patch() Patch {
	cfg := a.in.Config
	p := Patch{
		Status:   StatusNotComputed,
		Target:   cfg.PatchTarget,
		MinLines: cfg.MinLines,
		Blocking: cfg.Blocking,
		Layers:   []PatchLayer{},

		InformationalUntil: cfg.InformationalUntil,
	}
	if a.in.Diff == nil {
		return p
	}
	sum := &DiffSummary{Binary: []string{}, Deleted: []string{}, Renamed: []Rename{}, Unmeasured: []string{}}
	var changed []diff.File
	for _, f := range a.in.Diff {
		sum.Files++
		switch {
		case f.Status == diff.Deleted:
			sum.Deleted = append(sum.Deleted, f.Path)
			continue
		case f.Binary:
			sum.Binary = append(sum.Binary, f.Path)
		}
		if f.Status == diff.Renamed || f.Status == diff.Copied {
			sum.Renamed = append(sum.Renamed, Rename{From: f.OldPath, To: f.Path})
		}
		if !f.Binary && len(f.Added) > 0 {
			sum.AddedLines += len(f.Added)
			changed = append(changed, f)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	p.Diff = sum

	target := coverage.Hundredths(cfg.PatchTarget)
	union := map[string]map[int]bool{}
	inSomeLayer := map[string]bool{}
	anyPass, anyFail := false, false
	for _, ld := range a.data {
		pl := PatchLayer{Layer: ld.cfg.ID, Label: ld.cfg.Label, Files: []PatchFile{}}
		if !ld.measured {
			pl.Status = StatusNotMeasured
			p.Layers = append(p.Layers, pl)
			continue
		}
		gated := !ld.cfg.ReportOnly
		var lc coverage.Count
		for _, cf := range changed {
			f := ld.files[cf.Path]
			if f == nil {
				continue
			}
			inSomeLayer[cf.Path] = true
			var fc coverage.Count
			var unc []int
			for _, ln := range cf.Added {
				covered, coverable := f.Lines[ln]
				if !coverable {
					continue
				}
				fc.Total++
				if covered {
					fc.Covered++
				} else {
					unc = append(unc, ln)
				}
				if gated {
					if union[cf.Path] == nil {
						union[cf.Path] = map[int]bool{}
					}
					union[cf.Path][ln] = union[cf.Path][ln] || covered
				}
			}
			lc = lc.Add(fc)
			pl.Files = append(pl.Files, PatchFile{
				Path:             cf.Path,
				PatchCount:       patchCount(fc),
				Changed:          coverage.Ranges(cf.Added),
				UncoveredChanged: coverage.Ranges(unc),
				CoveredLines:     coverage.Ranges(f.LinesWhere(true)),
				UncoveredLines:   coverage.Ranges(f.LinesWhere(false)),
			})
		}
		pl.PatchCount = patchCount(lc)
		switch {
		case !gated:
			pl.Status = StatusReportOnly
		case lc.Total == 0 || lc.Total < int64(cfg.MinLines):
			pl.Status = StatusExempt
		case lc.AtLeast(target):
			pl.Status = StatusPass
			anyPass = true
		default:
			pl.Status = StatusFail
			anyFail = true
			if cfg.Blocking {
				thr := cfg.PatchTarget
				a.rep.Failures = append(a.rep.Failures, Failure{
					Scope: FailPatch, Layer: ld.cfg.ID, Measured: pl.Pct, Threshold: &thr,
					Message: fmt.Sprintf("%s patch coverage is %.2f%% (%d/%d changed lines), below the %s%% target",
						ld.cfg.ID, lc.Pct(), lc.Covered, lc.Total, trimFloat(cfg.PatchTarget)),
				})
			}
		}
		p.Layers = append(p.Layers, pl)
	}
	var overall coverage.Count
	for _, lines := range union {
		for _, covered := range lines {
			overall.Total++
			if covered {
				overall.Covered++
			}
		}
	}
	p.Overall = patchCount(overall)
	for _, cf := range changed {
		if !inSomeLayer[cf.Path] {
			sum.Unmeasured = append(sum.Unmeasured, cf.Path)
		}
	}
	switch {
	case anyFail:
		p.Status = StatusFail
	case anyPass:
		p.Status = StatusPass
	default:
		p.Status = StatusExempt
	}
	return p
}

func patchCount(c coverage.Count) PatchCount {
	pc := PatchCount{Covered: c.Covered, Total: c.Total}
	if c.Total > 0 {
		pc.Pct = ptr(c.Pct2())
	}
	return pc
}
