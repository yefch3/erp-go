package app

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 密码规则在两个地方各写了一遍：这里，和浏览器那边
// （frontend/src/lib/passwordPolicy.ts，让人边打边看见还差哪几条）。
//
// 两份实现会走散，而走散的样子特别难查：界面上四条全绿，提交却被拒——人只能
// 一遍遍试。所以样例只有一份（frontend/src/lib/passwordPolicy.cases.json），
// 两边读同一个文件。谁改了规则忘了改另一边，两边有一边会红。
//
// 这条测试不连库，跑在普通 go test 里。

type policyCases struct {
	ProblemToCode map[string]string `json:"problemToCode"`
	Cases         []struct {
		Why      string   `json:"why"`
		Password string   `json:"password"`
		Context  []string `json:"context"`
		Expect   string   `json:"expect"`
	} `json:"cases"`
}

func TestPasswordPolicyAgreesWithTheBrowsersCopy(t *testing.T) {
	raw, err := os.ReadFile("../../../../frontend/src/lib/passwordPolicy.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases policyCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Cases) == 0 {
		t.Fatal("样例文件是空的——是不是挪走了？")
	}
	seen := map[string]bool{}
	for _, c := range cases.Cases {
		seen[c.Expect] = true
		err := checkPasswordStrength(c.Password, c.Context...)
		if c.Expect == "ok" {
			if err != nil {
				t.Errorf("%s：这个密码该放行，却被拒了 —— %v", c.Why, err)
			}
			continue
		}
		want, ok := cases.ProblemToCode[c.Expect]
		if !ok {
			t.Errorf("%s：样例里的 %q 没有对应的错误码", c.Why, c.Expect)
			continue
		}
		if got := apierr.CodeFromError(err); got != want {
			t.Errorf("%s：该是 %s，得到 %s（%v）", c.Why, want, got, err)
		}
	}
	// 每一条规则都得有样例管着，否则某天松掉一条没人会知道。
	for problem := range cases.ProblemToCode {
		if !seen[problem] {
			t.Errorf("样例里没有一条会撞上 %s 的密码", problem)
		}
	}
	if !seen["ok"] {
		t.Error("样例里一个能通过的密码都没有——那只证明它什么都拒")
	}
}
