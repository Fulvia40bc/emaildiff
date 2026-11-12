//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wmDestroy    = 0x0002
	wmSize       = 0x0005
	wmKeyDown    = 0x0100
	wmCommand    = 0x0111
	wmSetFocus   = 0x0007
	wmDropFiles  = 0x0233
	lbAddString  = 0x0180
	lbReset      = 0x0184
	lbGetCursel  = 0x0188
	lbGetSelCnt  = 0x0190
	lbGetSelItm  = 0x0191
	bmGetCheck   = 0x00F0
	enChange     = 0x0300
	wsChild      = 0x40000000
	wsVisible    = 0x10000000
	wsBorder     = 0x00800000
	wsVScroll    = 0x00200000
	wsTabStop    = 0x00010000
	wsOverlapped = 0x00CF0000
	lbsNotify    = 0x0001
	lbsMultiSel  = 0x0008
	lbsNoIntH    = 0x0080
	bsButton     = 0x00000000
	bsCheck      = 0x00000002
	bsAutoCheck  = 0x00000003
	bsAutoRadio  = 0x00000009
	esAutoVScroll = 0x00000040
	gmemMoveable = 0x0002
	cfText       = 1
	cfUnicode    = 13
	ofnMustExist = 0x1800
	ofnOverwrite = 0x00000002
	gwlpWndProc  = ^uintptr(3)
	vkControl    = 0x11
	vkV          = 0x56
	tpmReturnCmd = 0x0100
	tpmNonNotify = 0x0080
	idLoadA      = 1
	idPasteA     = 2
	idLoadB      = 3
	idPasteB     = 4
	idAddToA     = 5
	idRemoveSel  = 6
	idSort       = 7
	idCompare    = 8
	idClear      = 9
	idOptNoCase  = 10
	idOptPlus    = 11
	idOptDomain  = 12
	idModeMiss   = 13
	idModeOnlyA  = 14
	idSaveReport = 15
	idCopyMiss   = 16
	idCopyReport = 17
	idSaveSess   = 18
	idLoadSess   = 19
	idRecent     = 20
	idFltA       = 21
	idFltC       = 22
	idFltB       = 23
	idRecentBase = 100
	clientW      = 1000
	clientH      = 620
)

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	shell32    = syscall.NewLazyDLL("shell32.dll")
	comdlg32   = syscall.NewLazyDLL("comdlg32.dll")
	pGetModule = kernel32.NewProc("GetModuleHandleW")
	pRegClass  = user32.NewProc("RegisterClassExW")
	pCreateWin = user32.NewProc("CreateWindowExW")
	pDefProc   = user32.NewProc("DefWindowProcW")
	pGetMsg    = user32.NewProc("GetMessageW")
	pTranslate = user32.NewProc("TranslateMessage")
	pDispatch  = user32.NewProc("DispatchMessageW")
	pPostQuit  = user32.NewProc("PostQuitMessage")
	pSendMsg   = user32.NewProc("SendMessageW")
	pMoveWin   = user32.NewProc("MoveWindow")
	pSetText   = user32.NewProc("SetWindowTextW")
	pGetText   = user32.NewProc("GetWindowTextW")
	pMsgBox    = user32.NewProc("MessageBoxW")
	pLoadCurs  = user32.NewProc("LoadCursorW")
	pLoadIcon  = user32.NewProc("LoadIconW")
	pAdjRect   = user32.NewProc("AdjustWindowRectEx")
	pSetLong   = user32.NewProc("SetWindowLongPtrW")
	pCallProc  = user32.NewProc("CallWindowProcW")
	pDragAcc   = shell32.NewProc("DragAcceptFiles")
	pDragQuery = shell32.NewProc("DragQueryFileW")
	pDragFin   = shell32.NewProc("DragFinish")
	pGAlloc    = kernel32.NewProc("GlobalAlloc")
	pGLock     = kernel32.NewProc("GlobalLock")
	pGUnlock   = kernel32.NewProc("GlobalUnlock")
	pGSize     = kernel32.NewProc("GlobalSize")
	pOpenClip  = user32.NewProc("OpenClipboard")
	pEmptyClip = user32.NewProc("EmptyClipboard")
	pSetClip   = user32.NewProc("SetClipboardData")
	pGetClip   = user32.NewProc("GetClipboardData")
	pClipAvail = user32.NewProc("IsClipboardFormatAvailable")
	pCloseClip = user32.NewProc("CloseClipboard")
	pGetKey    = user32.NewProc("GetKeyState")
	pGetCursor = user32.NewProc("GetCursorPos")
	pMkPopup   = user32.NewProc("CreatePopupMenu")
	pAppend    = user32.NewProc("AppendMenuW")
	pTrack     = user32.NewProc("TrackPopupMenu")
	pDestroyM  = user32.NewProc("DestroyMenu")
	pGetOpen   = comdlg32.NewProc("GetOpenFileNameW")
	pGetSave   = comdlg32.NewProc("GetSaveFileNameW")
)

var (
	hMain, hLeft, hCenter, hRight, hStatus uintptr
	hHdrL, hHdrC, hHdrR                    uintptr
	hFltL, hFltC, hFltB                    uintptr
	btns                                   [15]uintptr
	chkNoCase, chkPlus, chkDomain          uintptr
	radMiss, radOnlyA                      uintptr
	oldProc    = map[uintptr]uintptr{}
	left, right = newSide(), newSide()
	centerItems []string
	dispL, dispC, dispR []string
	fltL, fltC, fltB    string
	centerMode          int
	focusList           uintptr
	recents             []string
	opts                = normOptions{NoCase: true}
)

type wndClassEx struct {
	size          uint32
	style         uint32
	wndProc       uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	menuName      *uint16
	className     *uint16
	hIconSm       uintptr
}

type rect struct {
	l, t, r, b int32
}

type point struct {
	x, y int32
}

type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

type openFileName struct {
	lStructSize     uint32
	hwndOwner       uintptr
	hInstance       uintptr
	lpstrFilter     *uint16
	lpstrCustomFilt *uint16
	nMaxCustFilter  uint32
	nFilterIndex    uint32
	lpstrFile       *uint16
	nMaxFile        uint32
	lpstrFileTitle  *uint16
	nMaxFileTitle   uint32
	lpstrInitialDir *uint16
	lpstrTitle      *uint16
	flags           uint32
	nFileOffset     uint16
	nFileExtension  uint16
	lpstrDefExt     *uint16
	lCustData       uintptr
	lpfnHook        uintptr
	lpTemplateName  *uint16
	pvReserved      uintptr
	dwReserved      uint32
	flagsEx         uint32
}

func send(hwnd, m uintptr, w, l uintptr) uintptr {
	r, _, _ := pSendMsg.Call(hwnd, m, w, l)
	return r
}

func create(exStyle uintptr, className, title *uint16, style uint32, x, y, w, h int32, parent, menu, inst, param uintptr) uintptr {
	r, _, _ := pCreateWin.Call(exStyle, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), uintptr(style),
		uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(w)), uintptr(int32(h)), parent, menu, inst, param)
	return r
}

func utf16p(s string) *uint16 {
	return syscall.StringToUTF16Ptr(s)
}

func setText(hwnd uintptr, s string) {
	pSetText.Call(hwnd, uintptr(unsafe.Pointer(utf16p(s))))
}

func getText(hwnd uintptr) string {
	buf := make([]uint16, 1024)
	n, _, _ := pGetText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 1024)
	return syscall.UTF16ToString(buf[:n])
}

func status(s string) {
	setText(hStatus, s)
}

func errBox(msg string) {
	pMsgBox.Call(hMain, uintptr(unsafe.Pointer(utf16p(msg))), uintptr(unsafe.Pointer(utf16p("Email List Diff"))), 0x10)
}

func displayString(s *side, e string) string {
	if s.dups[normKey(e, opts)] {
		return e + "  (dup)"
	}
	return e
}

func refreshSide(hwnd uintptr, s *side, filter string) *[]string {
	disp := (*[]string)(nil)
	_ = disp
	send(hwnd, lbReset, 0, 0)
	var out []string
	fl := strings.ToLower(filter)
	for _, it := range s.items {
		if fl != "" && !strings.Contains(strings.ToLower(it), fl) {
			continue
		}
		out = append(out, it)
		send(hwnd, lbAddString, 0, uintptr(unsafe.Pointer(utf16p(displayString(s, it)))))
	}
	return &out
}

func refreshCenter() {
	send(hCenter, lbReset, 0, 0)
	dispC = dispC[:0]
	fl := strings.ToLower(fltC)
	for _, it := range centerItems {
		if fl != "" && !strings.Contains(strings.ToLower(it), fl) {
			continue
		}
		dispC = append(dispC, it)
		send(hCenter, lbAddString, 0, uintptr(unsafe.Pointer(utf16p(it))))
	}
	label := fmt.Sprintf("Missing from A - in B only (%d)", len(centerItems))
	if centerMode == 1 {
		label = fmt.Sprintf("Only in A - not in B (%d)", len(centerItems))
	}
	setText(hHdrC, label)
}

func refreshAll() {
	dispL = *refreshSide(hLeft, left, fltL)
	dispR = *refreshSide(hRight, right, fltB)
	refreshCenter()
	setText(hHdrL, fmt.Sprintf("List A - your emails (%d, %d dup)", len(left.items), len(left.dups)))
	setText(hHdrR, fmt.Sprintf("List B - other emails (%d, %d dup)", len(right.items), len(right.dups)))
}

func compare() {
	d := computeDiff(left, right, opts)
	if centerMode == 1 {
		centerItems = d.onlyInA
	} else {
		centerItems = d.onlyInB
	}
	union := len(d.onlyInA) + len(d.onlyInB) + d.common
	pct := 0
	if union > 0 {
		pct = d.common * 100 / union
	}
	refreshAll()
	status(fmt.Sprintf("A: %d | B: %d | common: %d | only in A: %d | only in B: %d | match: %d%%",
		len(left.items), len(right.items), d.common, len(d.onlyInA), len(d.onlyInB), pct))
}

func addRecent(path string) {
	for i, r := range recents {
		if r == path {
			recents = append(recents[:i], recents[i+1:]...)
			break
		}
	}
	recents = append([]string{path}, recents...)
	if len(recents) > 5 {
		recents = recents[:5]
	}
}

func mergeText(s *side, text, source string) {
	found := extractEmails(text)
	added := mergeEmails(s, found, opts)
	refreshAll()
	compare()
	status(fmt.Sprintf("Loaded %d emails from %s (%d new).", len(found), source, added))
}

func loadInto(side int, path string) {
	found, err := loadFileEmails(path)
	if err != nil {
		errBox(err.Error())
		return
	}
	s := left
	if side == 1 {
		s = right
	}
	added := mergeEmails(s, found, opts)
	addRecent(path)
	refreshAll()
	compare()
	status(fmt.Sprintf("Loaded %s: %d emails from %s (%d new).", map[int]string{0: "A", 1: "B"}[side], len(found), filepath.Base(path), added))
}

func copyText(s string) {
	u, _ := syscall.UTF16FromString(s)
	h, _, _ := pGAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		errBox("Clipboard allocation failed.")
		return
	}
	p, _, _ := pGLock.Call(h)
	if p != 0 {
		dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u))
		copy(dst, u)
		pGUnlock.Call(h)
	}
	pOpenClip.Call(0)
	pEmptyClip.Call(0)
	pSetClip.Call(cfUnicode, h)
	pCloseClip.Call(0)
	status(fmt.Sprintf("Copied %d characters to the clipboard.", len(s)))
}

func copyMissing() {
	if len(centerItems) == 0 {
		status("Nothing in the center column to copy.")
		return
	}
	copyText("Emails in center column:\r\n" + strings.Join(centerItems, "\r\n"))
}

func reportText() string {
	d := computeDiff(left, right, opts)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("List A (%d):\r\n", len(left.items)))
	b.WriteString(strings.Join(left.items, "\r\n"))
	b.WriteString("\r\n\r\nMissing from A, present in B (" + fmt.Sprint(len(d.onlyInB)) + "):\r\n")
	b.WriteString(strings.Join(d.onlyInB, "\r\n"))
	b.WriteString("\r\n\r\nOnly in A, not in B (" + fmt.Sprint(len(d.onlyInA)) + "):\r\n")
	b.WriteString(strings.Join(d.onlyInA, "\r\n"))
	b.WriteString("\r\n\r\nList B (" + fmt.Sprint(len(right.items)) + "):\r\n")
	b.WriteString(strings.Join(right.items, "\r\n"))
	return b.String()
}

func copyReport() {
	copyText(reportText())
}

func readClipboardText() string {
	r, _, _ := pClipAvail.Call(cfUnicode)
	if r == 0 {
		r, _, _ = pClipAvail.Call(cfText)
		if r == 0 {
			return ""
		}
	}
	pOpenClip.Call(0)
	defer pCloseClip.Call(0)
	format := uintptr(cfUnicode)
	if r == 0 {
		format = cfText
	}
	h, _, _ := pGetClip.Call(format)
	if h == 0 {
		return ""
	}
	p, _, _ := pGLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGUnlock.Call(h)
	sz, _, _ := pGSize.Call(h)
	if format == cfUnicode {
		n := int(sz) / 2
		return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), int(sz)))
}

func pasteInto(which int) {
	t := readClipboardText()
	if strings.TrimSpace(t) == "" {
		status("Clipboard has no text.")
		return
	}
	mergeText(map[int]*side{0: left, 1: right}[which], t, "clipboard")
}

func selectedStrings(hwnd uintptr, disp []string) []string {
	cnt := send(hwnd, lbGetSelCnt, 0, 0)
	if cnt == 0 {
		if cur := send(hwnd, lbGetCursel, 0, 0); cur != 0xFFFFFFFF && int(cur) < len(disp) {
			return []string{disp[cur]}
		}
		return nil
	}
	idx := make([]int32, cnt)
	send(hwnd, lbGetSelItm, uintptr(cnt), uintptr(unsafe.Pointer(&idx[0])))
	var out []string
	for _, i := range idx {
		if int(i) < len(disp) {
			out = append(out, disp[i])
		}
	}
	return out
}

func removeFromSide(s *side, vals []string) {
	for _, v := range vals {
		k := normKey(v, opts)
		if !s.seen[k] {
			continue
		}
		delete(s.seen, k)
		delete(s.dups, k)
		for i, it := range s.items {
			if normKey(it, opts) == k {
				s.items = append(s.items[:i], s.items[i+1:]...)
				break
			}
		}
	}
}

func addSelectedToA() {
	vals := selectedStrings(hCenter, dispC)
	if len(vals) == 0 {
		status("Select one or more emails in the center column first.")
		return
	}
	added := mergeEmails(left, vals, opts)
	compare()
	status(fmt.Sprintf("Added %d email(s) to list A.", added))
}

func removeSelected() {
	if send(hLeft, lbGetSelCnt, 0, 0) > 0 {
		removeFromSide(left, selectedStrings(hLeft, dispL))
		compare()
		status("Removed selected from list A.")
		return
	}
	if send(hRight, lbGetSelCnt, 0, 0) > 0 {
		removeFromSide(right, selectedStrings(hRight, dispR))
		compare()
		status("Removed selected from list B.")
		return
	}
	status("Select items in list A or B to remove.")
}

func sortBoth() {
	sort.Strings(left.items)
	sort.Strings(right.items)
	compare()
	status("Sorted lists A and B (A-Z).")
}

func clearAll() {
	left.reset()
	right.reset()
	centerItems = nil
	refreshAll()
	compare()
	status("Cleared. Drag & drop .xlsx/.csv/.txt onto a column, or use Load/Paste.")
}

func utf16Multi(parts ...string) *uint16 {
	var b []uint16
	for _, p := range parts {
		u, _ := syscall.UTF16FromString(p)
		b = append(b, u[:len(u)-1]...)
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0]
}

func pickFile(title string, save bool) string {
	var buf [1024]uint16
	ofn := openFileName{
		lpstrFilter: utf16Multi("Excel and text files", "*.xlsx;*.xlsm;*.csv;*.txt", "All files", "*.*"),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  utf16p(title),
		flags:       ofnMustExist,
	}
	if save {
		ofn.lpstrFilter = utf16Multi("Text file", "*.txt", "CSV file", "*.csv", "Session", "*.txt", "All files", "*.*")
		ofn.flags = ofnOverwrite
		ofn.lpstrDefExt = utf16p("txt")
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	proc := pGetOpen
	if save {
		proc = pGetSave
	}
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

func browse(side int) {
	if p := pickFile("Select a file", false); p != "" {
		loadInto(side, p)
	}
}

func saveReport() {
	p := pickFile("Save report as...", true)
	if p == "" {
		return
	}
	var content string
	if strings.ToLower(filepath.Ext(p)) == ".csv" {
		var sb strings.Builder
		sb.WriteString("email\r\n")
		for _, e := range centerItems {
			sb.WriteString(e + "\r\n")
		}
		content = sb.String()
	} else {
		content = reportText()
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		errBox(err.Error())
		return
	}
	status("Report saved to " + p)
}

func saveSession() {
	p := pickFile("Save session as...", true)
	if p == "" {
		return
	}
	if err := os.WriteFile(p, []byte(encodeSession(left, right)), 0644); err != nil {
		errBox(err.Error())
		return
	}
	status("Session saved to " + p)
}

func loadSession() {
	p := pickFile("Load session...", false)
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		errBox(err.Error())
		return
	}
	a, b, err := decodeSession(string(data))
	if err != nil {
		errBox(err.Error())
		return
	}
	left.reset()
	right.reset()
	mergeEmails(left, a, opts)
	mergeEmails(right, b, opts)
	compare()
	status("Session loaded from " + filepath.Base(p))
}

func showRecent() {
	if len(recents) == 0 {
		status("No recent files yet.")
		return
	}
	hm, _, _ := pMkPopup.Call()
	if hm == 0 {
		return
	}
	for i, r := range recents {
		pAppend.Call(hm, 0, uintptr(idRecentBase+i), uintptr(unsafe.Pointer(utf16p(fmt.Sprintf("%d %s", i+1, r)))))
	}
	var pt point
	pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
	which := 0
	if focusList == hRight {
		which = 1
	}
	sel, _, _ := pTrack.Call(hm, tpmReturnCmd|tpmNonNotify, uintptr(pt.x), uintptr(pt.y), 0, hMain, 0)
	pDestroyM.Call(hm)
	if sel >= idRecentBase && int(sel-idRecentBase) < len(recents) {
		loadInto(which, recents[sel-idRecentBase])
	}
}

func handleDrop(hwnd, wparam, lparam uintptr) {
	count, _, _ := pDragQuery.Call(wparam, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		pDragFin.Call(wparam)
		return
	}
	side := -1
	switch hwnd {
	case hLeft:
		side = 0
	case hRight:
		side = 1
	default:
		x := int(int16(uint32(lparam) & 0xFFFF))
		if x < clientW/2 {
			side = 0
		} else {
			side = 1
		}
	}
	if side == -1 {
		status("Drop files onto the left (A) or right (B) column.")
		pDragFin.Call(wparam)
		return
	}
	for i := uintptr(0); i < count; i++ {
		buf := make([]uint16, 1024)
		n, _, _ := pDragQuery.Call(wparam, i, uintptr(unsafe.Pointer(&buf[0])), 1024)
		if n > 0 {
			loadInto(side, syscall.UTF16ToString(buf[:n]))
		}
	}
	pDragFin.Call(wparam)
}

func layout(lparam uintptr) {
	cx := int(lparam & 0xFFFF)
	cy := int((lparam >> 16) & 0xFFFF)
	if cx < 560 || cy < 360 {
		return
	}
	w := (cx - 40) / 3
	if w < 140 {
		w = 140
	}
	listH := cy - 104 - 112
	if listH < 120 {
		listH = 120
	}
	pMoveWin.Call(hHdrL, 10, 54, uintptr(w), 20, 1)
	pMoveWin.Call(hHdrC, uintptr(20+w), 54, uintptr(w), 20, 1)
	pMoveWin.Call(hHdrR, uintptr(30+2*w), 54, uintptr(w), 20, 1)
	pMoveWin.Call(hFltL, 10, 78, uintptr(w), 22, 1)
	pMoveWin.Call(hFltC, uintptr(20+w), 78, uintptr(w), 22, 1)
	pMoveWin.Call(hFltB, uintptr(30+2*w), 78, uintptr(w), 22, 1)
	pMoveWin.Call(hLeft, 10, 104, uintptr(w), uintptr(listH), 1)
	pMoveWin.Call(hCenter, uintptr(20+w), 104, uintptr(w), uintptr(listH), 1)
	pMoveWin.Call(hRight, uintptr(30+2*w), 104, uintptr(w), uintptr(listH), 1)
	row1y := uintptr(cy - 104)
	row2y := uintptr(cy - 70)
	bw := uintptr(100)
	gap := uintptr(6)
	x := uintptr(10)
	for i := 0; i < 9; i++ {
		pMoveWin.Call(btns[i], x, row1y, bw, 28, 1)
		x += bw + gap
	}
	x = 10
	for i := 9; i < 15; i++ {
		pMoveWin.Call(btns[i], x, row2y, bw, 28, 1)
		x += bw + gap
	}
	pMoveWin.Call(hStatus, 10, uintptr(cy-34), uintptr(cx-20), 24, 1)
}

func readOpts() {
	opts.NoCase = send(chkNoCase, bmGetCheck, 0, 0) != 0
	opts.StripPlus = send(chkPlus, bmGetCheck, 0, 0) != 0
	opts.Domain = send(chkDomain, bmGetCheck, 0, 0) != 0
	centerMode = 0
	if send(radOnlyA, bmGetCheck, 0, 0) != 0 {
		centerMode = 1
	}
}

func wndProc(hwnd, m, wparam, lparam uintptr) uintptr {
	if old, ok := oldProc[hwnd]; ok {
		if m == wmDropFiles {
			handleDrop(hwnd, wparam, lparam)
			return 0
		}
		if m == wmSetFocus {
			focusList = hwnd
		}
		r, _, _ := pCallProc.Call(old, hwnd, uintptr(m), wparam, lparam)
		return r
	}
	switch m {
	case wmDestroy:
		pPostQuit.Call(0)
		return 0
	case wmDropFiles:
		handleDrop(hwnd, wparam, lparam)
		return 0
	case wmKeyDown:
		if wparam == vkV {
			if ks, _, _ := pGetKey.Call(vkControl); ks < 0 {
				side := 0
				if focusList == hRight {
					side = 1
				}
				pasteInto(side)
				return 0
			}
		}
	case wmCommand:
		id := wparam & 0xFFFF
		notify := (wparam >> 16) & 0xFFFF
		switch id {
		case idLoadA:
			browse(0)
		case idPasteA:
			pasteInto(0)
		case idLoadB:
			browse(1)
		case idPasteB:
			pasteInto(1)
		case idAddToA:
			addSelectedToA()
		case idRemoveSel:
			removeSelected()
		case idSort:
			sortBoth()
		case idCompare:
			compare()
		case idClear:
			clearAll()
		case idSaveReport:
			saveReport()
		case idCopyMiss:
			copyMissing()
		case idCopyReport:
			copyReport()
		case idSaveSess:
			saveSession()
		case idLoadSess:
			loadSession()
		case idRecent:
			showRecent()
		case idOptNoCase, idOptPlus, idOptDomain, idModeMiss, idModeOnlyA:
			readOpts()
			compare()
		case idFltA:
			if notify == enChange {
				fltL = getText(hFltL)
				dispL = *refreshSide(hLeft, left, fltL)
			}
		case idFltC:
			if notify == enChange {
				fltC = getText(hFltC)
				refreshCenter()
			}
		case idFltB:
			if notify == enChange {
				fltB = getText(hFltB)
				dispR = *refreshSide(hRight, right, fltB)
			}
		}
		return 0
	case wmSize:
		layout(lparam)
		return 0
	}
	r, _, _ := pDefProc.Call(hwnd, uintptr(m), wparam, lparam)
	return r
}

func subclass(hwnd, cb uintptr) {
	old, _, _ := pSetLong.Call(hwnd, gwlpWndProc, cb)
	oldProc[hwnd] = old
}

func main() {
	hInst, _, _ := pGetModule.Call(0)
	cb := syscall.NewCallback(wndProc)
	className := utf16p("EmailDiffMain")
	cursor, _, _ := pLoadCurs.Call(0, 32512)
	icon, _, _ := pLoadIcon.Call(0, 32512)
	wc := wndClassEx{
		size:          uint32(unsafe.Sizeof(wndClassEx{})),
		wndProc:       cb,
		hInstance:     hInst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: 6,
		className:     className,
	}
	if atom, _, _ := pRegClass.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		fmt.Println("RegisterClassEx failed")
		return
	}
	rc := rect{0, 0, clientW, clientH}
	pAdjRect.Call(uintptr(unsafe.Pointer(&rc)), wsOverlapped, 0, 0)
	hMain = create(0, className, utf16p("Email List Diff"), wsOverlapped|wsVisible, 100, 60, rc.r-rc.l, rc.b-rc.t, 0, 0, hInst, 0)
	if hMain == 0 {
		fmt.Println("CreateWindow failed")
		return
	}
	listClass := utf16p("LISTBOX")
	staticClass := utf16p("STATIC")
	btnClass := utf16p("BUTTON")
	editClass := utf16p("EDIT")
	listStyle := uint32(wsChild | wsVisible | wsBorder | wsVScroll | lbsNotify | lbsMultiSel | lbsNoIntH)
	create(0, staticClass, utf16p("Drag & drop .xlsx/.csv/.txt files onto a column, paste with Ctrl+V, or use Load/Paste buttons."), wsChild|wsVisible, 10, 6, 980, 18, hMain, 0, hInst, 0)
	chkNoCase = create(0, btnClass, utf16p("Case-insensitive"), wsChild|wsVisible|wsTabStop|bsAutoCheck, 10, 28, 150, 22, hMain, idOptNoCase, hInst, 0)
	chkPlus = create(0, btnClass, utf16p("Strip +tags"), wsChild|wsVisible|wsTabStop|bsAutoCheck, 165, 28, 130, 22, hMain, idOptPlus, hInst, 0)
	chkDomain = create(0, btnClass, utf16p("Domain only"), wsChild|wsVisible|wsTabStop|bsAutoCheck, 300, 28, 120, 22, hMain, idOptDomain, hInst, 0)
	radMiss = create(0, btnClass, utf16p("Center: in B, not A"), wsChild|wsVisible|wsTabStop|bsAutoRadio, 440, 28, 170, 22, hMain, idModeMiss, hInst, 0)
	radOnlyA = create(0, btnClass, utf16p("Center: only in A"), wsChild|wsVisible|wsTabStop|bsAutoRadio, 615, 28, 150, 22, hMain, idModeOnlyA, hInst, 0)
	send(chkNoCase, 0x00F1, 1, 0)
	send(radMiss, 0x00F1, 1, 0)
	hHdrL = create(0, staticClass, utf16p("List A - your emails (0)"), wsChild|wsVisible, 10, 54, 300, 20, hMain, 0, hInst, 0)
	hHdrC = create(0, staticClass, utf16p("Missing from A - in B only (0)"), wsChild|wsVisible, 330, 54, 300, 20, hMain, 0, hInst, 0)
	hHdrR = create(0, staticClass, utf16p("List B - other emails (0)"), wsChild|wsVisible, 650, 54, 300, 20, hMain, 0, hInst, 0)
	hFltL = create(0, editClass, nil, wsChild|wsVisible|wsBorder|wsTabStop|esAutoVScroll, 10, 78, 300, 22, hMain, idFltA, hInst, 0)
	hFltC = create(0, editClass, nil, wsChild|wsVisible|wsBorder|wsTabStop|esAutoVScroll, 330, 78, 300, 22, hMain, idFltC, hInst, 0)
	hFltB = create(0, editClass, nil, wsChild|wsVisible|wsBorder|wsTabStop|esAutoVScroll, 650, 78, 300, 22, hMain, idFltB, hInst, 0)
	hLeft = create(0, listClass, nil, listStyle, 10, 104, 300, 400, hMain, 0, hInst, 0)
	hCenter = create(0, listClass, nil, listStyle, 330, 104, 300, 400, hMain, 0, hInst, 0)
	hRight = create(0, listClass, nil, listStyle, 650, 104, 300, 400, hMain, 0, hInst, 0)
	row1 := []struct {
		id    uintptr
		title string
	}{
		{idLoadA, "Load A..."}, {idPasteA, "Paste A"}, {idLoadB, "Load B..."}, {idPasteB, "Paste B"},
		{idAddToA, "Add to A"}, {idRemoveSel, "Remove sel"}, {idSort, "Sort A-Z"}, {idCompare, "Compare"}, {idClear, "Clear"},
	}
	row2 := []struct {
		id    uintptr
		title string
	}{
		{idSaveReport, "Save report"}, {idCopyMiss, "Copy missing"}, {idCopyReport, "Copy report"},
		{idSaveSess, "Save session"}, {idLoadSess, "Load session"}, {idRecent, "Recent"},
	}
	x := int32(10)
	for i, d := range row1 {
		btns[i] = create(0, btnClass, utf16p(d.title), wsChild|wsVisible|wsTabStop|bsButton, x, 516, 100, 28, hMain, d.id, hInst, 0)
		x += 106
	}
	x = 10
	for i, d := range row2 {
		btns[9+i] = create(0, btnClass, utf16p(d.title), wsChild|wsVisible|wsTabStop|bsButton, x, 550, 100, 28, hMain, d.id, hInst, 0)
		x += 106
	}
	hStatus = create(0, staticClass, utf16p("Ready."), wsChild|wsVisible, 10, 586, 980, 24, hMain, 0, hInst, 0)
	subclass(hLeft, cb)
	subclass(hCenter, cb)
	subclass(hRight, cb)
	pDragAcc.Call(hMain, 1)
	pDragAcc.Call(hLeft, 1)
	pDragAcc.Call(hCenter, 1)
	pDragAcc.Call(hRight, 1)
	compare()
	var m msg
	for {
		r, _, _ := pGetMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 {
			break
		}
		pTranslate.Call(uintptr(unsafe.Pointer(&m)))
		pDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}
