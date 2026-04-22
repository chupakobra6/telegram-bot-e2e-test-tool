package textcase

import "testing"

func TestRenderIncludesCancelFlowByDefault(t *testing.T) {
	t.Parallel()

	commands := Render("@bot", "кефир до пятницы", "↩️ Отмена", 12000)
	if len(commands) != 6 {
		t.Fatalf("len(commands) = %d, want 6", len(commands))
	}
	if commands[0].Chat != "@bot" {
		t.Fatalf("select_chat chat = %q, want @bot", commands[0].Chat)
	}
	if commands[4].Action != "click_button" || commands[4].ButtonText != "↩️ Отмена" {
		t.Fatalf("cancel command = %+v", commands[4])
	}
}

func TestRenderOmitsCancelFlowWhenButtonEmpty(t *testing.T) {
	t.Parallel()

	commands := Render("@bot", "кефир до пятницы", "", 12000)
	if len(commands) != 4 {
		t.Fatalf("len(commands) = %d, want 4", len(commands))
	}
}
