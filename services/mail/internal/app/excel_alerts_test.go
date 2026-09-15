package app

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// CloudWatch 上的告警按日志里的 event 字段匹配（deploy/aws/05-alerts.sh）。
// 这里钉住两边一致：代码里每个 event 脚本都认识，脚本里每个 event 代码都在打。
// 少一个是「有失败没人知道」，多一个是「一条永远不会响的告警」——后者更坏，
// 它看着像有人在盯着。
func TestAlertScriptKnowsEveryExcelEvent(t *testing.T) {
	script, err := os.ReadFile("../../../../deploy/aws/05-alerts.sh")
	if err != nil {
		t.Fatal(err)
	}
	inCode := []string{
		excelEventStorageUnreachable, excelEventStorageUnavailable, excelEventRowWriteFailed,
		excelEventResultRecovered, excelEventResultUnreachable, excelEventSweepRemoveFailed,
	}
	for _, event := range inCode {
		if !strings.Contains(string(script), `"`+event+`"`) && !strings.Contains(string(script), " "+event+" ") {
			t.Errorf("代码会打 event=%s，但 05-alerts.sh 里没有这条告警", event)
		}
	}
	// 脚本里定义告警的两种写法：`excel <event> "<说明>"` 一行，或者直接写
	// 在过滤器 pattern 里的 `$.event = "<event>"`。注释里提到的文件名
	// （excel_jobs.go）不算。
	inScript := regexp.MustCompile(`(?m)^excel\s+(excel_[a-z_]+)|\$\.event = "(excel_[a-z_]+)"`).
		FindAllStringSubmatch(string(script), -1)
	if len(inScript) == 0 {
		t.Fatal("05-alerts.sh 里一条 Excel 告警都没找到——是不是改了写法？")
	}
	for _, m := range inScript {
		event := m[1] + m[2]
		if !slices.Contains(inCode, event) {
			t.Errorf("05-alerts.sh 里有 %s，但代码从来不打这个 event——那条告警永远不会响", event)
		}
	}
}
