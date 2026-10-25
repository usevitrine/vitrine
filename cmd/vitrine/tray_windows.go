package main

// The Windows installer's Vitrine.exe: this program built without a console (-H windowsgui, main.gui set). It runs
// the server and sits in the notification area: a click opens Vitrine in the browser, its menu has the address for
// phones, the files folder, the log, Start with Windows, Restart and Quit. One runs at a time (a second start opens
// the browser and leaves). Started with --background (at sign-in) it doesn't open the browser; "Vitrine.exe --quit"
// asks the running one to stop and waits for it (the installer and uninstaller do).

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/usevitrine/vitrine/internal/config"
	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/proc"
)

//go:embed vitrine.ico
var trayIcon []byte

// mutexName is held while Vitrine.exe runs (the installer waits for it to go before replacing the program).
const mutexName = `Local\Vitrine`

// quitName is the event a running Vitrine.exe waits on: set, it stops (--quit).
const quitName = `Local\VitrineQuit`

// runKey is where Windows keeps the programs started at sign-in (this user's).
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func runTray(args []string) int {
	background := slices.Contains(args, "--background")
	if slices.Contains(args, "--quit") {
		quitRunning()
		return 0
	}
	c, err := config.Load("", os.Getenv, config.Config{})
	if err != nil {
		alert("Vitrine", err.Error())
		return 1
	}
	if err := i18n.SetDefault(c.Language); err != nil {
		c.Language = ""
	}
	l := i18n.For(i18n.Default())
	local, phones := addresses(c.Listen)

	name, _ := windows.UTF16PtrFromString(mutexName)
	mutex, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if !background {
			open(local)
		}
		return 0
	}
	t := &tray{l: l, c: c, local: local, phones: phones, done: make(chan struct{})}
	t.ctx, t.stop = context.WithCancel(context.Background())
	// The installer's update: started, it waits for this program to go (the mutex), updates it and starts it again.
	setupName = "Vitrine-Setup-%s-x64.exe"
	runSetup = func(path string) error {
		cmd := exec.Command(path, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
		if err := cmd.Start(); err != nil {
			return err
		}
		cmd.Process.Release()
		go func() {
			time.Sleep(time.Second) // (the page hears the update started)
			t.quit()
		}()
		return nil
	}
	if ev, err := windows.CreateEvent(nil, 0, 0, windows.StringToUTF16Ptr(quitName)); err == nil {
		go func() {
			windows.WaitForSingleObject(ev, windows.INFINITE)
			t.quit()
		}()
	}
	go t.serve(background)
	systray.Run(t.ready, func() {})
	windows.CloseHandle(mutex)
	if t.relaunch {
		self, err := os.Executable()
		if err == nil {
			err = exec.Command(self, "--background").Start()
		}
		if err != nil {
			alert("Vitrine", err.Error())
			return 1
		}
	}
	return 0
}

type tray struct {
	l             i18n.Lang
	c             config.Config
	local, phones string // the addresses to open (phones: "" when only this computer can)
	ctx           context.Context
	stop          context.CancelFunc
	done          chan struct{} // closed when the server has stopped
	relaunch      bool          // quit to start again (Restart)
}

// serve runs the server until it stops: asked to (Quit, Restart, an update), or the web app's Restart (after a
// restore: the program starts again, fresh), or it failed (said in a message box).
func (t *tray) serve(background bool) {
	go func() {
		if waitUp(t.ctx, t.local) && !background {
			open(t.local)
		}
		if t.ctx.Err() == nil {
			systray.SetTooltip(t.l.T("tray.running", "address", strings.TrimPrefix(t.local, "http://")))
		}
	}()
	err := cmdServe(t.ctx, nil, io.Discard, io.Discard)
	close(t.done)
	switch {
	case t.ctx.Err() != nil: // (asked to stop)
	case err != nil:
		alert(t.l.T("tray.failed"), err.Error())
	default: // the web app's Restart
		t.relaunch = true
	}
	systray.Quit()
}

func (t *tray) ready() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("Vitrine")
	systray.SetTooltip(t.l.T("tray.starting"))
	systray.SetOnTapped(func() { open(t.local) })

	openItem := systray.AddMenuItem(t.l.T("tray.open"), "")
	var copyItem *systray.MenuItem
	if t.phones != "" {
		copyItem = systray.AddMenuItem(t.l.T("tray.copyAddress"), t.phones)
	}
	systray.AddSeparator()
	filesItem := systray.AddMenuItem(t.l.T("tray.openFiles"), t.c.FilesDir)
	logItem := systray.AddMenuItem(t.l.T("tray.openLog"), "")
	systray.AddSeparator()
	startupItem := systray.AddMenuItemCheckbox(t.l.T("tray.startup"), "", startsAtSignIn())
	systray.AddSeparator()
	restartItem := systray.AddMenuItem(t.l.T("tray.restart"), "")
	quitItem := systray.AddMenuItem(t.l.T("tray.quit"), "")

	var copied <-chan struct{}
	if copyItem != nil {
		copied = copyItem.ClickedCh
	}
	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				open(t.local)
			case <-copied:
				if copyText(t.phones) == nil {
					copyItem.SetTitle(t.l.T("tray.copied"))
					time.AfterFunc(2*time.Second, func() { copyItem.SetTitle(t.l.T("tray.copyAddress")) })
				}
			case <-filesItem.ClickedCh:
				os.MkdirAll(t.c.FilesDir, 0o755)
				open(t.c.FilesDir)
			case <-logItem.ClickedCh:
				open(filepath.Join(t.c.DataDir, "logs", "vitrine.log"))
			case <-startupItem.ClickedCh:
				on := !startupItem.Checked()
				if err := setStartAtSignIn(on); err != nil {
					alert("Vitrine", err.Error())
				} else if on {
					startupItem.Check()
				} else {
					startupItem.Uncheck()
				}
			case <-restartItem.ClickedCh:
				t.relaunch = true
				go t.quit()
			case <-quitItem.ClickedCh:
				go t.quit()
			case <-t.done:
				return
			}
		}
	}()
}

// quit stops the server (it finishes what it's writing: up to ~10 s) and then the program.
func (t *tray) quit() {
	systray.SetTooltip(t.l.T("tray.stopping"))
	t.stop()
	select {
	case <-t.done:
	case <-time.After(20 * time.Second):
	}
	systray.Quit()
}

// quitRunning asks the running Vitrine.exe to stop and waits (up to 30 s) until it has.
func quitRunning() {
	ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(quitName))
	if err != nil {
		return // (none running)
	}
	windows.SetEvent(ev)
	windows.CloseHandle(ev)
	for range 150 {
		m, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
		if err != nil {
			return // (gone)
		}
		windows.CloseHandle(m)
		time.Sleep(200 * time.Millisecond)
	}
}

// addresses are the URLs Vitrine answers on for this computer and, when it listens on every network, for phones
// and other devices on the network (by this computer's address on it; "" when it has none or listens locally only).
func addresses(listen string) (local, phones string) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://localhost:8080", ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		local = "http://localhost:" + port
		if ip := lanIP(); ip != "" {
			phones = "http://" + net.JoinHostPort(ip, port)
		}
	case "127.0.0.1", "::1", "localhost":
		local = "http://localhost:" + port
	default:
		local = "http://" + net.JoinHostPort(host, port)
		phones = local
	}
	return local, phones
}

// lanIP is this computer's private IPv4 address: the one its default route goes out from (no packet is sent), else
// the first an interface has.
func lanIP() string {
	if conn, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if ip.IsPrivate() {
			return ip.String()
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return ""
}

// waitUp waits for the server to answer (true), or ctx to end.
func waitUp(ctx context.Context, url string) bool {
	host := strings.TrimPrefix(url, "http://")
	for ctx.Err() == nil {
		if conn, err := net.DialTimeout("tcp", host, time.Second); err == nil {
			conn.Close()
			return true
		}
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

// open opens a web address in the browser, or a folder or file as Explorer would.
func open(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(target)
	windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// alert shows a message box (this program has no console to print to).
func alert(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONWARNING)
}

// copyText puts text on the clipboard (with Windows' clip).
func copyText(text string) error {
	cmd := proc.Hide(exec.Command("clip"))
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func startsAtSignIn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("Vitrine")
	return err == nil
}

// setStartAtSignIn adds or removes Vitrine from the programs Windows starts at sign-in (the installer's option too).
func setStartAtSignIn(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("Vitrine"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("Vitrine", `"`+self+`" --background`) // (as the installer writes it)
}
