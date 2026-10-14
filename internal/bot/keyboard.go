package bot

const btnStartAll = "🚀 Start (all directions)"

// reply keyboard rows of plain-string buttons (allowed by the Bot API)
type tgReplyKeyboard struct {
	Keyboard        [][]string `json:"keyboard"`
	ResizeKeyboard  bool       `json:"resize_keyboard"`
	OneTimeKeyboard bool       `json:"one_time_keyboard"`
}

type tgRemoveKeyboard struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

func keyboardOf(items []string, perRow int) tgReplyKeyboard {
	var rows [][]string
	for len(items) > 0 {
		n := min(perRow, len(items))
		rows = append(rows, items[:n])
		items = items[n:]
	}
	return tgReplyKeyboard{Keyboard: rows, ResizeKeyboard: true, OneTimeKeyboard: true}
}
