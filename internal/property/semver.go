package property

import "fmt"

// Semantic parameters govern replay. Changing any parameter creates a new
// semantic version; dependents are invalidated by scope, never replaced
// silently. The values here are the *differences* replayers must branch on.
type SemParams struct {
	Version string `json:"version"`

	// ZAsFalse: how a literal z operand is read in a boolean expression.
	// v1 treats z as unknown (strict four-state, IEEE-like sampled value);
	// a revision can set this true to test parameter-driven invalidation.
	ZAsFalse bool `json:"z_as_false"`

	// DisableXIffy: handling of disable iff (cond) when cond reads unknown.
	// false (v1): disable iff cond is "not known true"  => an unknown cond
	//             yields an explicit unknown (disabled? unknown), never a
	//             silent pass;
	// true (v2):  unknown cond disables the attempt (de-facto simulator
	//             behavior some teams choose) — results therefore differ on
	//             reset-glitch-into-X windows, which is the version-diff case.
	DisableXDisables bool `json:"disable_x_disables"`

	// DisableSampling: "preponed" (sample before the edge, SVA style) or
	// "postponed". Fixed sampling semantics default to preponed; a revision
	// shifting this invalidates edge-adjacent attempts only.
	DisableSampling string `json:"disable_sampling"`

	// NonOverlapGap: clock ticks between antecedent and consequent for |=>.
	// Always 1 in fixed semantics; retained for explicit dependency wiring.
	NonOverlapGap int `json:"non_overlap_gap"`
}

// SemV1 is the baseline semantic version shipped with the case pack.
func SemV1() SemParams {
	return SemParams{
		Version:          "1.0.0",
		ZAsFalse:         false,
		DisableXDisables: false,
		DisableSampling:  "preponed",
		NonOverlapGap:    1,
	}
}

// SemV2 is the parameter revision: only disable-iff handling of X changes,
// so only disable-iff-bearing properties are invalidated.
func SemV2() SemParams {
	return SemParams{
		Version:          "2.0.0",
		ZAsFalse:         false,
		DisableXDisables: true,
		DisableSampling:  "preponed",
		NonOverlapGap:    1,
	}
}

// Validate checks internal consistency of a parameter set.
func (s SemParams) Validate() error {
	if s.Version == "" {
		return fmt.Errorf("semantic version is empty")
	}
	if s.DisableSampling != "preponed" && s.DisableSampling != "postponed" {
		return fmt.Errorf("unsupported disable sampling %q", s.DisableSampling)
	}
	if s.NonOverlapGap != 1 {
		return fmt.Errorf("fixed semantics require non_overlap_gap=1, got %d", s.NonOverlapGap)
	}
	return nil
}

// DiffScope describes which AST features must be re-evaluated when moving
// from s to other. Empty slices mean "nothing invalidated".
type DiffScope struct {
	All      bool     `json:"all"`
	Features []string `json:"features"` // "z-literal","disable-iff-x","disable-edge",...
	Changed  []string `json:"changed_fields"`
}

// Diff computes the invalidation scope of replacing s with other.
func (s SemParams) Diff(other SemParams) DiffScope {
	d := DiffScope{}
	if s.ZAsFalse != other.ZAsFalse {
		d.Features = append(d.Features, "z-literal")
		d.Changed = append(d.Changed, "z_as_false")
	}
	if s.DisableXDisables != other.DisableXDisables {
		d.Features = append(d.Features, "disable-iff-x")
		d.Changed = append(d.Changed, "disable_x_disables")
	}
	if s.DisableSampling != other.DisableSampling {
		d.Features = append(d.Features, "disable-edge")
		d.Changed = append(d.Changed, "disable_sampling")
	}
	if s.NonOverlapGap != other.NonOverlapGap {
		d.All = true
		d.Changed = append(d.Changed, "non_overlap_gap")
	}
	return d
}

// Affects reports whether the diff scope changes a property with the given
// feature flags (used for localized invalidation).
func (d DiffScope) Affects(hasFeature func(string) bool) bool {
	if d.All {
		return true
	}
	for _, f := range d.Features {
		if hasFeature(f) {
			return true
		}
	}
	return false
}
