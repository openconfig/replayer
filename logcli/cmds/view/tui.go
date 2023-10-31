// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package view

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/kylelemons/godebug/pretty"
	"github.com/openconfig/replayer/internal"
	"github.com/rivo/tview"
	"google.golang.org/protobuf/proto"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

func runTUI(l []*internal.Event) error {
	// Define widgets
	t := &tui{
		l:        l,
		msgView:  tview.NewTextView(),
		table:    tview.NewTable(),
		flex:     tview.NewFlex(),
		app:      tview.NewApplication(),
		controls: tview.NewTextView(),
		search:   tview.NewInputField(),
	}

	t.setupTable()
	t.setupMessageView()

	// Setup controls
	t.controls.SetText("[Arrow Keys, jk]: Move cursor  [Enter]: View entry  [Esc, q]: Close entry  [Tab]: Change focus  [/]: Search  [n]: Go to next match (when in message view)")

	// Setup search
	t.setupSearch()

	// Setup layout
	mainPane := tview.NewFlex().
		AddItem(t.table, 0, 2, true).
		AddItem(t.msgView, 0, 3, true)
	t.flex.SetDirection(tview.FlexRow).
		AddItem(mainPane, 0, 1, true).
		AddItem(t.controls, 2, 0, false)

	t.app.SetRoot(t.flex, true).EnableMouse(false)
	return t.app.Run()
}

type tui struct {
	l []*internal.Event

	nextMatchStartIndex int

	msgView  *tview.TextView
	table    *tview.Table
	flex     *tview.Flex
	app      *tview.Application
	controls *tview.TextView
	search   *tview.InputField

	previousFocus tview.Primitive
}

func (t *tui) setupTable() {
	t.table.
		SetSelectable(true, false).
		SetTitle(file).
		SetTitleAlign(tview.AlignCenter).
		SetBorder(true)

	headerCell := func(s string) *tview.TableCell {
		return tview.NewTableCell(s).
			SetBackgroundColor(tcell.ColorGray).
			SetSelectable(false).
			SetExpansion(1)
	}
	t.table.SetCell(0, 0, headerCell("Index"))
	t.table.SetCell(0, 1, headerCell("Timestamp"))
	t.table.SetCell(0, 2, headerCell("Message Type"))
	t.table.SetFixed(1, 0)

	for i, entry := range t.l {
		t.table.SetCellSimple(i+1, 0, strconv.Itoa(i))
		t.table.SetCellSimple(i+1, 1, entry.Timestamp.String())
		t.table.SetCellSimple(i+1, 2, fmt.Sprintf("%T", entry.Message))
	}

	selectCell := func(r int, _ int) {
		if r == 0 {
			return
		}
		t.nextMatchStartIndex = 0
		entry := t.l[r-1]
		t.msgView.SetTitle(fmt.Sprintf("Entry %d  %v  %v", r-1, entry.Timestamp, fmt.Sprintf("%T", entry.Message)))
		t.msgView.SetText("Loading...")
		t.app.SetFocus(t.msgView)
		go t.app.QueueUpdateDraw(func() {
			t.msgView.SetText(prettySprintMessage(entry.Message)).ScrollToBeginning()
		})
	}

	t.table.SetSelectedFunc(selectCell)
	t.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			if t.msgView.GetText(false) != "" {
				t.app.SetFocus(t.msgView)
			}
			return nil
		}
		return event
	})
}

func (t *tui) setupMessageView() {
	t.msgView.SetScrollable(true).SetBorder(true).SetBorderPadding(0, 0, 2, 0)
	t.msgView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyRune && event.Rune() == 'q') {
			t.msgView.SetText("")
			t.msgView.SetTitle("")
			t.app.SetFocus(t.table)
			return nil
		}
		if event.Key() == tcell.KeyTab {
			t.app.SetFocus(t.table)
			return nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == 'n' {
			t.nextHighlight()
			return nil
		}
		return event
	})
}

func (t *tui) nextHighlight() {
	search := t.search.GetText()
	if search == "" {
		return
	}

	txt := t.msgView.GetText(true)
	nextMatchFromStart := strings.Index(txt[t.nextMatchStartIndex:], search)
	if nextMatchFromStart == -1 {
		// Wrap around and check from start of text.
		t.nextMatchStartIndex = 0
		nextMatchFromStart = strings.Index(txt, search)
		if nextMatchFromStart == -1 {
			return // not in the text
		}
	}
	nextMatchIdx := t.nextMatchStartIndex + nextMatchFromStart

	txtWithTag := fmt.Sprintf(`%v["hl"]%v[""]%v`,
		txt[:nextMatchIdx],
		txt[nextMatchIdx:nextMatchIdx+len(search)],
		txt[nextMatchIdx+len(search):])

	t.nextMatchStartIndex = nextMatchIdx + len(search)

	go t.app.QueueUpdateDraw(func() {
		t.msgView.SetRegions(true)
		t.msgView.SetText(txtWithTag)
		t.msgView.Highlight("hl")
		t.msgView.ScrollToHighlight()
	})
}

func (t *tui) setupSearch() {
	t.search.SetLabel("Search: ")
	t.search.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			go t.executeSearch()
		}
	})
	t.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == '/' {
			t.flex.RemoveItem(t.controls)
			t.flex.AddItem(t.search, 2, 0, true)
			t.search.SetText("")
			t.previousFocus = t.app.GetFocus()
			t.app.SetFocus(t.search)
			return nil
		}
		return event
	})
}

func (t *tui) executeSearch() {
	match := make(chan int)
	wg := sync.WaitGroup{}
	for i, entry := range t.l {
		wg.Add(1)
		go func() {
			defer wg.Done()
			txt := prettySprintMessage(entry.Message)
			if strings.Contains(txt, t.search.GetText()) {
				match <- i
			}
		}()
	}

	go func() {
		wg.Wait()
		close(match)
	}()

	// Clear old highlights.
	go t.app.QueueUpdateDraw(func() {
		for i := 1; i < t.table.GetRowCount(); i++ {
			for j := 0; j < t.table.GetColumnCount(); j++ {
				t.table.GetCell(i, j).SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
			}
		}
	})
	// Highlight matched rows.
	for i := range match {
		go t.app.QueueUpdateDraw(func() {
			for j := 0; j < t.table.GetColumnCount(); j++ {
				t.table.GetCell(i+1, j).SetBackgroundColor(tcell.ColorYellow)
			}
		})
	}

	// Restore controls.
	go t.app.QueueUpdateDraw(func() {
		t.flex.RemoveItem(t.search)
		t.flex.AddItem(t.controls, 2, 0, true)
		t.app.SetFocus(t.previousFocus)
	})
}

func prettySprintMessage(m proto.Message) string {
	pc := pretty.Config{
		Formatter: map[reflect.Type]any{
			reflect.TypeOf(&gnmipb.TypedValue_JsonIetfVal{}): formatJSONValue},
	}
	return pc.Sprint(m)
}

func formatJSONValue(v *gnmipb.TypedValue_JsonIetfVal) string {
	b := map[string]any{}
	if err := json.Unmarshal(v.JsonIetfVal, &b); err != nil {
		return "ERROR UNMARSHAL"
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "ERROR MARSHAL"
	}
	return string(out)
}
