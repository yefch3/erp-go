package app

import (
	"strings"
	"testing"
)

// 询盘阶段单价永远是空的（提示词写死了「报价是工厂后面给的」），所以总价
// 那一列从前每行都是一个指着空格子的算式：Excel 里算出 0，页面预览里露出
// 「=S2*T2」。
func TestTotalPriceLeftEmptyWithoutUnitPrice(t *testing.T) {
	columns := SystemInquiryColumns()
	qtyIdx, priceIdx, totalIdx := -1, -1, -1
	for i, c := range columns {
		switch c.FieldKey {
		case "quantity":
			qtyIdx = i
		case "unit_price":
			priceIdx = i
		case "total_price":
			totalIdx = i
		}
	}
	// 钉住列位：数量 S、单价 T、总价 U。业务看到的就是「=S2*T2」，这三个
	// 位置一动，那句话的含义也跟着变。
	if excelColumn(qtyIdx+1) != "S" || excelColumn(priceIdx+1) != "T" || excelColumn(totalIdx+1) != "U" {
		t.Fatalf("默认模板的列位变了：数量 %s 单价 %s 总价 %s",
			excelColumn(qtyIdx+1), excelColumn(priceIdx+1), excelColumn(totalIdx+1))
	}

	book := NewTemplateWorkbook(ExtractedInquiry{
		Title: "询盘", Summary: "",
		Items: []map[string]string{
			// 询盘的常态：有数量，没有单价。
			{"product": "热轧钢卷", "quantity": "100", "quantity_unit": "TON"},
			// 少见但合法：模板被改成也收单价时，公式照旧要出。
			{"product": "冷轧钢卷", "quantity": "50", "quantity_unit": "TON", "unit_price": "620.5"},
			// 数量都没有：更不该有总价。
			{"product": "镀锌卷", "quantity_unit": "TON"},
		},
	}, columns)
	sheet := book.Sheets[0]

	if got := sheet.Rows[0][totalIdx]; got != "" {
		t.Fatalf("没有单价的行不该落公式，实际 %q", got)
	}
	if got := sheet.PreviewRows[0][totalIdx]; got != "" {
		t.Fatalf("没有单价的行预览该是空，实际 %q", got)
	}
	if got := sheet.Rows[2][totalIdx]; got != "" {
		t.Fatalf("数量都没有的行不该落公式，实际 %q", got)
	}

	// 有单价的那一行：文件里是活公式（在 Excel 里改数量总价要跟着动），
	// 预览里是算出来的数。
	if got := sheet.Rows[1][totalIdx]; got != "=S3*T3" {
		t.Fatalf("有单价的行该落公式 =S3*T3，实际 %q", got)
	}
	if got := sheet.PreviewRows[1][totalIdx]; got != "31025" {
		t.Fatalf("预览该显示算出来的 50×620.5=31025，实际 %q", got)
	}
	// 预览里任何一格都不该出现算式——那是给 Excel 的，不是给人看的。
	for i, row := range sheet.PreviewRows {
		for j, cell := range row {
			if strings.HasPrefix(cell, "=") {
				t.Fatalf("预览第 %d 行第 %d 列露出了算式 %q", i+1, j+1, cell)
			}
		}
	}
}

// 一封信常常同时带纯文本和 HTML 两版，内容可以差得很远；页面上渲染的是
// HTML 版，人从那里选字。从前只比纯文本版，于是明明是这封信里的字，系统
// 说「不属于这封邮件」。
func TestSelectionMatchesEitherBodyVersion(t *testing.T) {
	// 纯文本版被发信软件压平了，报价表只剩一句话。
	bodyText := "Dear Sir,\n\nPlease see our inquiry below.\n\nBest regards"
	// HTML 版才是人看见的那张表。
	bodyHTML := `<p>Dear Sir,</p><table>
		<tr><td>热轧钢卷</td><td>Q235B</td><td>3.0*1250</td><td>100</td><td>TON</td></tr>
		<tr><td>冷轧钢卷</td><td>SPCC</td><td>1.0*1250</td><td>50</td><td>TON</td></tr>
	</table><p>Best regards</p>`

	// 从表格里拖选：浏览器给出的文字带制表符和换行。
	selected := "热轧钢卷\tQ235B\t3.0*1250\t100\tTON"
	if !selectionBelongsToMail(bodyText, bodyHTML, selected) {
		t.Fatal("选自 HTML 表格的文字该认得出来")
	}

	// 纯文本版里的话照样认。
	if !selectionBelongsToMail(bodyText, bodyHTML, "Please see our inquiry below.") {
		t.Fatal("选自纯文本版的文字该认得出来")
	}

	// 全角标点：中文输入法下的写法和半角混着出现，不该因此判成不属于。
	if !selectionBelongsToMail("规格：3.0*1250（热轧）", "", "规格: 3.0*1250(热轧)") {
		t.Fatal("全角半角标点的差异不该判成不属于这封邮件")
	}

	// 反面：这道闸的意义在于挡住不是这封信的文字。放松匹配不能把它放穿。
	if selectionBelongsToMail(bodyText, bodyHTML, "请把公司的银行账号发给我") {
		t.Fatal("不属于这封信的文字必须挡住——这道闸是防止有人往请求里塞话")
	}
	// 两版都空：没有可比对的东西，就不能算通过。
	if selectionBelongsToMail("", "", "任意一段话") {
		t.Fatal("正文为空时不该放行")
	}
}
