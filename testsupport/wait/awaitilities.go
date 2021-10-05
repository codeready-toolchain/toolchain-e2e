package wait

func NewAwaitilities(hostAwait *HostAwaitility, memberAwaitilities ...*MemberAwaitility) Awaitilities {
	return Awaitilities{
		hostAwaitility:     hostAwait,
		memberAwaitilities: memberAwaitilities,
	}
}

type Awaitilities struct {
	hostAwaitility     *HostAwaitility
	memberAwaitilities []*MemberAwaitility
}

func (a Awaitilities) Host() *HostAwaitility {
	return a.hostAwaitility
}

func (a Awaitilities) Member1() *MemberAwaitility {
	return a.memberAwaitilities[0]
}

func (a Awaitilities) Member2() *MemberAwaitility {
	return a.memberAwaitilities[1]
}

func (a Awaitilities) AllMembers() []*MemberAwaitility {
	return a.memberAwaitilities
}
