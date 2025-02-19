package spaceprovisionerconfig

import (
	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates/conditions"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/predicates/object"
)

// That is a "constructor" function to instantiate and initialize an instance of
// SpaceProvisionerConfigPredicates interface.
//
// The name is a bit "funny" but will read nicely when used:
//
// spaceprovisionerconfig.That().HasName("asdf").ReferencesToolchainCluster("asdf")...
func That() SpaceProvisionerConfigPredicates {
	ret := &collector{}
	ret.ObjectPredicates.EmbedInto(ret, &ret.preds)
	ret.ConditionPredicates.EmbedInto(ret, &ret.preds,
		func(spc *v1alpha1.SpaceProvisionerConfig) *[]v1alpha1.Condition {
			return &spc.Status.Conditions
		})
	return ret
}

// SpaceProvisionerConfigPredicates is an interface defining all the predicates
// that can be applied on SpaceProvisionerConfig objects.
type SpaceProvisionerConfigPredicates interface {
	// This is the actual "top-level" predicate collector, so we need to make sure we implement that interface
	predicates.PredicateCollector[*v1alpha1.SpaceProvisionerConfig]

	// SpaceProvisionerConfigs are CRDs so we can embed the generic object predicates
	object.Predicates[SpaceProvisionerConfigPredicates, *v1alpha1.SpaceProvisionerConfig]

	// SpaceProvisionerConfigs use conditions in their status so we can embed the generic condition predicates
	conditions.Predicates[SpaceProvisionerConfigPredicates, *v1alpha1.SpaceProvisionerConfig]

	// These predicates are specific to SPCs

	ReferencesToolchainCluster(tc string) SpaceProvisionerConfigPredicates
}

// collector is a private impl of the SpaceProvisionerConfigPredicates interface. This is so
// that we force the use of the That function in this package that correctly wires stuff up.
type collector struct {
	// embed in the impl of the object predicates
	object.ObjectPredicates[SpaceProvisionerConfigPredicates, *v1alpha1.SpaceProvisionerConfig]

	// embed the impl of the condition predicates
	conditions.ConditionPredicates[SpaceProvisionerConfigPredicates, *v1alpha1.SpaceProvisionerConfig]

	// this is where all the predicates will be collected so that we can implement the predicate collector
	// interface
	preds []predicates.Predicate[*v1alpha1.SpaceProvisionerConfig]
}

func (p *collector) ReferencesToolchainCluster(tc string) SpaceProvisionerConfigPredicates {
	p.preds = append(p.preds, &referencesToolchainCluster{tc: tc})
	return p
}

func (p *collector) Predicates() []predicates.Predicate[*v1alpha1.SpaceProvisionerConfig] {
	return p.preds
}

type referencesToolchainCluster struct {
	tc string
}

func (p *referencesToolchainCluster) Matches(spc *v1alpha1.SpaceProvisionerConfig) (bool, error) {
	return spc.Spec.ToolchainCluster == p.tc, nil
}

func (p *referencesToolchainCluster) FixToMatch(spc *v1alpha1.SpaceProvisionerConfig) (*v1alpha1.SpaceProvisionerConfig, error) {
	copy := spc.DeepCopy()
	copy.Spec.ToolchainCluster = p.tc
	return copy, nil
}
