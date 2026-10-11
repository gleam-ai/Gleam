package agent

// bi 一句界面文案的中英两份。
//
// 为什么由后端自带英文，而不是让前端拿中文回查词表：台账与候补卡上的句子是
// **插值拼出来的**（主机名、次数、路径都嵌在句中），整节点精确匹配永远接不住；
// 前端若用正则去拆后端句子再重组，同一句话就有了两个 owner，后端改一个字
// 前端的拆法就静默失配。把英文说法放在中文说法旁边，谁改句子谁顺手补另一半，
// 漂移在同一个编辑动作里就能被看见。
//
// 「两种说法」不等于「两个口径」：什么时候说这句话、数字怎么读，仍归各处只写
// 一份；这里只承认同一句话有两种语言的说法。英文界面开口要英文时由 pick 给出。
// 配套判据：TestConnections_EnglishLedgerHasNoCJK / TestCues_EnglishViewHasNoCJK——
// 英文输出里出现任何 CJK（含全角标点）即红，这份账不需要人眼盯。
type bi struct {
	zh string
	en string
}

// pick 按界面语言取说法。
func (b bi) pick(en bool) string {
	if en {
		return b.en
	}
	return b.zh
}
