package assertions

// Not used at the moment - just an experiment how to play with custom testingT instances
// and influence the output
func ObjectUnderTest(t AssertT, obj any) AssertT {
	return &objectT{
		AssertT: t,
		Object:  obj,
	}
}

type objectT struct {
	AssertT
	Object         any
	objectReported bool
}

func (t *objectT) Errorf(format string, args ...any) {
	t.Helper()
	if !t.objectReported {
		t.Errorf("Object failed one or more assertions\n%+v", t.Object) //nolint: testifylint
		t.objectReported = true
	}
	t.AssertT.Errorf(format, args...)
}
