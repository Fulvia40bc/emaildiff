//go:build windows

package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmCommand     = 0x0111
	wmDropFiles   = 0x0233
	lbAddString   = 0x0180
	lbReset       = 0x0184
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsBorder      = 0x00800000
	wsVScroll     = 0x00200000
	wsTabStop     = 0x00010000
	wsOverlapped  = 0x00CF0000
	lbsNotify     = 0x0001
	lbsNoIntH     = 0x0080
	gmemMoveable  = 0x0002
	cfUnicodeText = 13
	ofnMustExist  = 0x1800
	gwlpWndProc   = ^uintptr(3)
	idLoadA       = 1
	idLoadB       = 2
	idCompare     = 3
	idCopyMiss    = 4
	idCopyReport  = 5
	idClear       = 6
	clientW       = 960
	clientH       = 570
)

var (
	kernel32   = syscall.NewLazyDLL("kernel32.dll")
	user32     = syscall.NewLazyDLL("user32.dll")
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
	pOpenClip  = user32.NewProc("OpenClipboard")
	pEmptyClip = user32.NewProc("EmptyClipboard")
	pSetClip   = user32.NewProc("SetClipboardData")
	pCloseClip = user32.NewProc("CloseClipboard")
	pGetOpen   = comdlg32.NewProc("GetOpenFileNameW")
)

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	hMain   uintptr
	hLeft   uintptr
	hCenter uintptr
	hRight  uintptr
	hStatus uintptr
	hHdrL   uintptr
	hHdrC   uintptr
	hHdrR   uintptr
	btns    [6]uintptr
	oldProc = map[uintptr]uintptr{}
	left    = listState{seen: map[string]bool{}}
	right   = listState{seen: map[string]bool{}}
	center  []string
)

type listState struct {
	items []string
	seen  map[string]bool
}

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

type openFileName struct {
	lStructSize      uint32
	hwndOwner        uintptr
	hInstance        uintptr
	lpstrFilter      *uint16
	lpstrCustomFilt  *uint16
	nMaxCustFilter   uint32
	nFilterIndex     uint32
	lpstrFile        *uint16
	nMaxFile         uint32
	lpstrFileTitle   *uint16
	nMaxFileTitle    uint32
	lpstrInitialDir  *uint16
	lpstrTitle       *uint16
	flags            uint32
	nFileOffset      uint16
	nFileExtension   uint16
	lpstrDefExt      *uint16
	lCustData        uintptr
	lpfnHook         uintptr
	lpTemplateName   *uint16
	pvReserved       uintptr
	dwReserved       uint32
	flagsEx          uint32
}

func send(hwnd, msg, w, l uintptr) uintptr {
	r, _, _ := pSendMsg.Call(hwnd, uintptr(msg), w, l)
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

type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

func setText(hwnd uintptr, s string) {
	pSetText.Call(hwnd, uintptr(unsafe.Pointer(utf16p(s))))
}

func status(s string) {
	setText(hStatus, s)
}

func errBox(hwnd uintptr, msg string) {
	pMsgBox.Call(hwnd, uintptr(unsafe.Pointer(utf16p(msg))), uintptr(unsafe.Pointer(utf16p("Email List Diff"))), 0x10)
}

func extractEmails(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range emailRe.FindAllString(text, -1) {
		k := strings.ToLower(m)
		if !seen[k] {
			seen[k] = true
			out = append(out, m)
		}
	}
	return out
}

func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var out []string
	inSi, inT := false, false
	var cur strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSi = true
				cur.Reset()
			}
			if t.Name.Local == "t" && inSi {
				inT = true
			}
		case xml.CharData:
			if inT {
				cur.Write([]byte(t))
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			}
			if t.Name.Local == "si" && inSi {
				out = append(out, cur.String())
				inSi = false
			}
		}
	}
	return out, nil
}

func readWorksheetCells(f *zip.File, shared []string) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var cells []string
	cellT := ""
	inV, inT := false, false
	var v, is strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				cellT = ""
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellT = a.Value
					}
				}
				v.Reset()
				is.Reset()
			case "v":
				inV = true
			case "t":
				inT = true
			}
		case xml.CharData:
			if inV {
				v.Write([]byte(t))
			}
			if inT {
				is.Write([]byte(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				val := ""
				if cellT == "s" {
					if idx, err := strconv.Atoi(strings.TrimSpace(v.String())); err == nil && idx >= 0 && idx < len(shared) {
						val = shared[idx]
					}
				} else {
					val = v.String() + is.String()
				}
				cells = append(cells, val)
			}
		}
	}
	return cells, nil
}

func xlsxEmails(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var shared []string
	var sheets []*zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			s, err := readSharedStrings(f)
			if err != nil {
				return nil, err
			}
			shared = s
		case strings.HasPrefix(f.Name, "xl/worksheets/") && strings.HasSuffix(f.Name, ".xml"):
			sheets = append(sheets, f)
		}
	}
	var all []string
	seen := map[string]bool{}
	for _, f := range sheets {
		cells, err := readWorksheetCells(f, shared)
		if err != nil {
			return nil, err
		}
		for _, c := range cells {
			for _, e := range extractEmails(c) {
				k := strings.ToLower(e)
				if !seen[k] {
					seen[k] = true
					all = append(all, e)
				}
			}
		}
	}
	return all, nil
}

func loadFileEmails(path string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".xlsx", ".xlsm":
		return xlsxEmails(path)
	case ".xls":
		return nil, fmt.Errorf("%s: legacy .xls is not supported, please save as .xlsx", filepath.Base(path))
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return extractEmails(string(data)), nil
	}
}

func refreshList(hwnd uintptr, items []string) {
	send(hwnd, lbReset, 0, 0)
	for _, it := range items {
		send(hwnd, lbAddString, 0, uintptr(unsafe.Pointer(utf16p(it))))
	}
}

func loadInto(side int, path string) {
	found, err := loadFileEmails(path)
	if err != nil {
		errBox(hMain, err.Error())
		return
	}
	st := &left
	label := "A"
	if side == 1 {
		st = &right
		label = "B"
	}
	added := 0
	for _, e := range found {
		k := strings.ToLower(e)
		if !st.seen[k] {
			st.seen[k] = true
			st.items = append(st.items, e)
			added++
		}
	}
	hwnd := hLeft
	if side == 1 {
		hwnd = hRight
	}
	refreshList(hwnd, st.items)
	compare()
	status(fmt.Sprintf("Loaded %s: %d emails from %s (%d new).", label, len(found), filepath.Base(path), added))
}

func compare() {
	center = center[:0]
	for _, e := range right.items {
		if !left.seen[strings.ToLower(e)] {
			center = append(center, e)
		}
	}
	onlyLeft := 0
	for _, e := range left.items {
		if !right.seen[strings.ToLower(e)] {
			onlyLeft++
		}
	}
	common := len(left.items) - onlyLeft
	refreshList(hCenter, center)
	status(fmt.Sprintf("A: %d | B: %d | common: %d | only in A: %d | missing from A (in center): %d",
		len(left.items), len(right.items), common, onlyLeft, len(center)))
}

func copyText(s string) {
	u, _ := syscall.UTF16FromString(s)
	h, _, _ := pGAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		errBox(hMain, "Clipboard allocation failed.")
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
	pSetClip.Call(cfUnicodeText, h)
	pCloseClip.Call(0)
	status(fmt.Sprintf("Copied %d characters to the clipboard.", len(s)))
}

func copyMissing() {
	if len(center) == 0 {
		status("Nothing in the center column to copy.")
		return
	}
	copyText("Emails missing from list A:\r\n" + strings.Join(center, "\r\n"))
}

func copyReport() {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("List A (%d):\r\n", len(left.items)))
	b.WriteString(strings.Join(left.items, "\r\n"))
	b.WriteString("\r\n\r\nMissing from A, present in B (" + strconv.Itoa(len(center)) + "):\r\n")
	b.WriteString(strings.Join(center, "\r\n"))
	b.WriteString("\r\n\r\nList B (" + strconv.Itoa(len(right.items)) + "):\r\n")
	b.WriteString(strings.Join(right.items, "\r\n"))
	copyText(b.String())
}

func clearAll() {
	left = listState{seen: map[string]bool{}}
	right = listState{seen: map[string]bool{}}
	center = nil
	refreshList(hLeft, nil)
	refreshList(hCenter, nil)
	refreshList(hRight, nil)
	status("Cleared. Drag & drop .xlsx/.csv/.txt files onto the left or right column.")
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

func browse(side int) {
	var buf [1024]uint16
	ofn := openFileName{
		lpstrFilter: utf16Multi("Excel and text files", "*.xlsx;*.xlsm;*.csv;*.txt", "All files", "*.*"),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  utf16p("Select a file"),
		flags:       ofnMustExist,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetOpen.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return
	}
	loadInto(side, syscall.UTF16ToString(buf[:]))
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
	case hCenter:
		side = -1
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
	if cx < 480 || cy < 300 {
		return
	}
	w := (cx - 40) / 3
	if w < 120 {
		w = 120
	}
	h := cy - 56 - 92
	if h < 100 {
		h = 100
	}
	pMoveWin.Call(hHdrL, 10, 32, uintptr(w), 20, 1)
	pMoveWin.Call(hHdrC, uintptr(20+w), 32, uintptr(w), 20, 1)
	pMoveWin.Call(hHdrR, uintptr(30+2*w), 32, uintptr(w), 20, 1)
	pMoveWin.Call(hLeft, 10, 56, uintptr(w), uintptr(h), 1)
	pMoveWin.Call(hCenter, uintptr(20+w), 56, uintptr(w), uintptr(h), 1)
	pMoveWin.Call(hRight, uintptr(30+2*w), 56, uintptr(w), uintptr(h), 1)
	by := uintptr(cy - 64)
	xs := [6]uintptr{10, 170, 330, 490, 650, 810}
	for i, b := range btns {
		pMoveWin.Call(b, xs[i], by, 150, 30, 1)
	}
	pMoveWin.Call(hStatus, 10, uintptr(cy-28), uintptr(cx-20), 22, 1)
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if old, ok := oldProc[hwnd]; ok {
		if msg == wmDropFiles {
			handleDrop(hwnd, wparam, lparam)
			return 0
		}
		r, _, _ := pCallProc.Call(old, hwnd, uintptr(msg), wparam, lparam)
		return r
	}
	switch msg {
	case wmDestroy:
		pPostQuit.Call(0)
		return 0
	case wmDropFiles:
		handleDrop(hwnd, wparam, lparam)
		return 0
	case wmCommand:
		switch wparam & 0xFFFF {
		case idLoadA:
			browse(0)
		case idLoadB:
			browse(1)
		case idCompare:
			compare()
		case idCopyMiss:
			copyMissing()
		case idCopyReport:
			copyReport()
		case idClear:
			clearAll()
		}
		return 0
	case wmSize:
		layout(lparam)
		return 0
	}
	r, _, _ := pDefProc.Call(hwnd, uintptr(msg), wparam, lparam)
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
	w := rc.r - rc.l
	h := rc.b - rc.t
	hMain = create(0, className, utf16p("Email List Diff"), wsOverlapped|wsVisible, 100, 100, w, h, 0, 0, hInst, 0)
	if hMain == 0 {
		fmt.Println("CreateWindow failed")
		return
	}
	listClass := utf16p("LISTBOX")
	staticClass := utf16p("STATIC")
	btnClass := utf16p("BUTTON")
	listStyle := uint32(wsChild | wsVisible | wsBorder | wsVScroll | lbsNotify | lbsNoIntH)
	hHdrL = create(0, staticClass, utf16p("List A - your emails"), wsChild|wsVisible, 10, 32, 300, 20, hMain, 0, hInst, 0)
	hHdrC = create(0, staticClass, utf16p("Missing from A (present in B only)"), wsChild|wsVisible, 330, 32, 300, 20, hMain, 0, hInst, 0)
	hHdrR = create(0, staticClass, utf16p("List B - other emails"), wsChild|wsVisible, 650, 32, 300, 20, hMain, 0, hInst, 0)
	hLeft = create(0, listClass, nil, listStyle, 10, 56, 300, 440, hMain, 0, hInst, 0)
	hCenter = create(0, listClass, nil, listStyle, 330, 56, 300, 440, hMain, 0, hInst, 0)
	hRight = create(0, listClass, nil, listStyle, 650, 56, 300, 440, hMain, 0, hInst, 0)
	hint := create(0, staticClass, utf16p("Drag & drop .xlsx / .csv / .txt files onto the left or right column, or use the Load buttons."), wsChild|wsVisible, 10, 8, 940, 20, hMain, 0, hInst, 0)
	_ = hint
	btnDefs := []struct {
		id    uintptr
		title string
		x     uintptr
	}{
		{idLoadA, "Load A...", 10},
		{idLoadB, "Load B...", 170},
		{idCompare, "Compare", 330},
		{idCopyMiss, "Copy missing", 490},
		{idCopyReport, "Copy report", 650},
		{idClear, "Clear", 810},
	}
	for i, d := range btnDefs {
		btns[i] = create(0, btnClass, utf16p(d.title), wsChild|wsVisible|wsTabStop, int32(d.x), 504, 150, 30, hMain, d.id, hInst, 0)
	}
	hStatus = create(0, staticClass, utf16p("Ready."), wsChild|wsVisible, 10, 542, 940, 22, hMain, 0, hInst, 0)
	subclass(hLeft, cb)
	subclass(hCenter, cb)
	subclass(hRight, cb)
	pDragAcc.Call(hMain, 1)
	pDragAcc.Call(hLeft, 1)
	pDragAcc.Call(hCenter, 1)
	pDragAcc.Call(hRight, 1)
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
