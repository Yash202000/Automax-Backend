package services
import "testing"
func TestZZ(t *testing.T){
 spec,_:=masterSpecFor(KpiMasterAwardSubCriterion)
 for _,h:=range []string{"Award Criterion *","Sub No. *","Name (English) *"}{
  n:=normHeader(h)
  for _,s:=range masterColSpecs(spec){ if s.match(n){t.Logf("%q -> %s",h,s.key)} }
 }
}
