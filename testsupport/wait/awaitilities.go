package wait

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func NewAwaitilities(t *testing.T, hostAwait *HostAwaitility, memberAwaitilities ...*MemberAwaitility) Awaitilities {
	return Awaitilities{
		t:                  t,
		hostAwaitility:     hostAwait,
		memberAwaitilities: memberAwaitilities,
	}
}

type Awaitilities struct {
	t                  *testing.T
	hostAwaitility     *HostAwaitility
	memberAwaitilities []*MemberAwaitility
}

func (a Awaitilities) Host() *HostAwaitility {
	return a.hostAwaitility
}

type memberSelector func(*MemberAwaitility) bool

func (a Awaitilities) SecondMember(m *MemberAwaitility) bool {
	return m.ClusterName == a.memberAwaitilities[1].ClusterName
}

func (a Awaitilities) Member(selectors ...memberSelector) *MemberAwaitility {
	require.NotEmpty(a.t, a.memberAwaitilities, "there are no initialized member awaitilities")
	for _, selector := range selectors {
		for _, m := range a.memberAwaitilities {
			if selector(m) {
				return m
			}
		}
	}
	return a.memberAwaitilities[0]
}

func (a Awaitilities) AllMembers() []*MemberAwaitility {
	return a.memberAwaitilities
}
