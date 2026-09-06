// validate.go
package loader

import (
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Validate runs cross-reference checks that require the full manifest in hand.
// It does not enforce slot-accepts (that needs the global registry to know about
// other contributors' intent kinds); slot validation runs in registry.Register.
func Validate(m *contract.ContractManifest, wardens contract.WardenRegistry) error {
	intentByName := map[string]contract.Intent{}
	for _, in := range m.Intents {
		if _, dup := intentByName[in.Name]; dup {
			return fmt.Errorf("intent %q declared twice", in.Name)
		}
		if err := validateKindCapability(in); err != nil {
			return err
		}
		if err := validateWarden(in.Requires, wardens); err != nil {
			return fmt.Errorf("intent %q: %w", in.Name, err)
		}
		intentByName[in.Name] = in
	}
	// Validate query refs
	for name, q := range m.Queries {
		if _, ok := intentByName[q.Intent]; !ok {
			// allow refs to other-contributor intents; flag only same-contributor mistakes
			// Heuristic: if the name looks like "{contributor}.{rest}" with a different
			// contributor, skip; otherwise fail. Slice (b) tightens this.
			if !looksCrossContributor(q.Intent, m.Contributor.Name) {
				return fmt.Errorf("query %q: intent %q not declared in this contributor", name, q.Intent)
			}
		}
	}
	return nil
}

func validateKindCapability(in contract.Intent) error {
	want := map[contract.IntentKind]contract.Capability{
		contract.IntentKindQuery:        contract.CapRead,
		contract.IntentKindCommand:      contract.CapWrite,
		contract.IntentKindSubscription: contract.CapRead,
	}
	if w, ok := want[in.Kind]; ok && in.Capability != w {
		return fmt.Errorf("intent %q: kind=%s requires capability=%s, got %s", in.Name, in.Kind, w, in.Capability)
	}
	return nil
}

func validateWarden(p contract.Predicate, wardens contract.WardenRegistry) error {
	if p.Warden == "" {
		return nil
	}
	if _, ok := wardens.Get(p.Warden); !ok {
		return fmt.Errorf("references unknown warden %q", p.Warden)
	}
	return nil
}

func looksCrossContributor(intentName, ownContributor string) bool {
	// Convention: "auth.linkedAccount" — first dotted segment is contributor name.
	for i := 0; i < len(intentName); i++ {
		if intentName[i] == '.' {
			return intentName[:i] != ownContributor
		}
	}
	return false
}
