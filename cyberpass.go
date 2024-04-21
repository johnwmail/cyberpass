package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"golang.org/x/term"
)

var debug bool
var cyberark_url string
var username, password string
var tomorrow string
var buildTime, commitHash, version string
var browser = flag.Bool("b", false, "Enable browser mode")
var hostnameFile = flag.String("f", "hostname.txt", "The hostname.txt file")
var usage = flag.Bool("h", false, "This usage manual")
var ticketID = flag.String("t", "noreason", "The ticket ID")

func debugPrintf(format string, args ...interface{}) {
	if !debug {
		return
	}

	fmt.Fprintf(os.Stderr, format, args...)
}

func main() {
	var hostname, ipaddr, ansible_password string
	var credential string = "empty"
	var loggedin bool = false

	if os.Getenv("_CYBERDEBUG") != "" {
		debug = true
		debugPrintf("_CYBERDEBUG mode: enabled\n")
	}

	// Parse the command-line arguments
	flag.Parse()

	if *usage {
		fmt.Printf("Build time: %s\n", buildTime)
		fmt.Printf("Build version: %s\n", version)
		fmt.Printf("Build commitHash: %s\n\n", commitHash)
		flag.Usage()
		os.Exit(0)
	}

	today := time.Now()
	now := today.Format("2006-01-02_15-04-05")
	nextDay := today.AddDate(0, 0, 1)
	tomorrow = nextDay.Format("1/2/2006")
	inventoryFile := "inventory-" + now + ".ini"

	hostFilefd := openHostFile(*hostnameFile)
	defer hostFilefd.Close()

	inventory, err := os.OpenFile(inventoryFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer inventory.Close()
	fmt.Fprintf(inventory, "[all:vars]\n")
	fmt.Fprintf(inventory, "ansible_user=sysmgr\n")
	//fmt.Fprintf(inventory, "ansible_ssh_common_args='-J jump'\n")
	fmt.Fprintf(inventory, "\n\n[appserver]\n")

	// initial chromedp
	ctx, cancel := initChromedp()
	if ctx == nil {
		return
	}
	defer cancel()

	// Reverted: end with "[0-9]"
	validName := regexp.MustCompile("^rh[a-z0-9]+$")
	scanner := bufio.NewScanner(hostFilefd)
	for scanner.Scan() {
		hostname = strings.ToLower(scanner.Text())
		parts := strings.Fields(hostname)
		if len(parts) > 0 {
			hostname = parts[0]
		} else {
			debugPrintf("line: %s, len(parts) unexpected: %d\n", hostname, len(parts))
			continue
		}

		match := validName.MatchString(hostname)
		if !match {
			debugPrintf("hostname: %s not valid and len(parts): %d\n", hostname, len(parts))
			continue
		}

		if !loggedin {
			// login to cyberark
			loginCyberArk(ctx)
			loggedin = true
		}

		// goto front page first
		gotoFrontPage(ctx)

		if !searchHost(ctx, hostname) {
			debugPrintf("hostname: %s cannot find in searchHost\n", hostname)
			continue
		}
		if !selectHost(ctx, hostname, &ipaddr) {
			debugPrintf("hostname: %s cannot find in selecHost\n", hostname)
			continue
		}
		if !dropdownMenu(ctx, hostname) {
			debugPrintf("hostname: %s cannot find in dropdownMenu\n", hostname)
			continue
		}
		credential = copyPassword(ctx, hostname)
		if credential != "copy password error" {
			ansible_password = credential
			ansible_password = strings.ReplaceAll(ansible_password, "'", `\'`)
			ansible_password = strings.ReplaceAll(ansible_password, `"`, `\"`)
			fmt.Printf("%s  ansible_host=%s  ansible_password=\"'%s'\"  # credential => %s\n", hostname, ipaddr, ansible_password, credential)
			fmt.Fprintf(inventory, "%s  ansible_host=%s  ansible_password=\"'%s'\"  # credential => %s\n", hostname, ipaddr, ansible_password, credential)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	if credential == "empty" {
		inventory.Close()
		if err := os.RemoveAll(inventoryFile); err != nil {
			log.Fatal(err)
		} else {
			fmt.Printf("\nNo host found in: %s.\n", *hostnameFile)
		}
	} else {
		fmt.Printf("\nInventory file: %s generated.\n", inventoryFile)
	}

	chromedp.Cancel(ctx)
}

func RunWithTimeOut(ctx *context.Context, timeout time.Duration, tasks chromedp.Tasks) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		timeoutContext, cancel := context.WithTimeout(ctx, timeout*time.Second)
		defer cancel()
		return tasks.Do(timeoutContext)
	}
}

func copyPassword(ctx context.Context, hostname string) string {
	debugPrintf("Go to copyPassword\n")
	displayPass := ".account-password-display"
	var password string

	err := chromedp.Run(ctx,
		chromedp.Text(displayPass, &password, chromedp.NodeVisible),
	)
	if err != nil {
		password = "copy password error"
		fmt.Printf("%s: %s\n", hostname, password)
	} else {
		debugPrintf("%s: %s\n", hostname, password)
	}
	chromedp.Run(ctx,
		chromedp.Click(`//button[contains(text(), 'Close')]`),
	)
	return password
}

func dropdownMenu(ctx context.Context, hostname string) bool {
	debugPrintf("Go to dropdownMenu\n")
	var err error

	// request approval
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible("from-time", chromedp.ByID)},
		),
	)
	if err == nil {
		if *ticketID == "noreason" {
			fmt.Printf("%s: reqeust approval, but ticketID: \"%s\"\n", hostname, *ticketID)
			return false
		}
		fmt.Printf("%s: reqeust approval with ticket-id: \"%s\"\n", hostname, *ticketID)
		return requestApproval(ctx, hostname)
	}
	// request approval

	// pre-approval
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible("input#reason", chromedp.BySearch)},
		),
	)
	if err == nil {
		debugPrintf("dropdownMenu: pre-approval\n")
		err = chromedp.Run(ctx,
			chromedp.Click(`input#reason`),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.KeyEvent(kb.ArrowDown),
			chromedp.KeyEvent(kb.ArrowDown),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Click("div.x-combo-list-item.x-combo-selected"),
			chromedp.KeyEvent(kb.Enter),
		)
		if err != nil {
			log.Fatal(err)
		}
		err = chromedp.Run(ctx,
			RunWithTimeOut(&ctx, 2,
				chromedp.Tasks{chromedp.Click("#ext-gen284")},
			),
		)
		if err != nil {
			log.Fatal(err)
		}
		return true
	}
	// pre-approval

	// approved
	displayPass := ".account-password-display"
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible(displayPass, chromedp.BySearch)},
		),
	)
	if err == nil {
		fmt.Printf("%s: approved\n", hostname)
		return true
	}
	// approved

	// waiting approval
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible("pvBody_PageTemplate_innerHolder_ctrlRequestDetails_Details_lblRequestStatusValue", chromedp.BySearch)},
		),
	)
	if err == nil {
		fmt.Printf("%s: Waiting approval\n", hostname)
		gotoCyberArk(ctx)
		return false
	}
	// waiting approval

	return false
}

func requestApproval(ctx context.Context, hostname string) bool {
	debugPrintf("Go to requestApproval\n")
	err := chromedp.Run(ctx,
		chromedp.SetValue("from-time", "9:00 AM", chromedp.ByID),
	)
	if err != nil {
		log.Fatal(err)
	}
	err = chromedp.Run(ctx,
		chromedp.SetValue("to-date", tomorrow, chromedp.ByID),
	)
	if err != nil {
		log.Fatal(err)
	}
	err = chromedp.Run(ctx,
		chromedp.SetValue("to-time", "9:00 AM", chromedp.ByID),
	)
	if err != nil {
		log.Fatal(err)
	}
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible("input#reason", chromedp.BySearch)},
		),
	)
	err = chromedp.Run(ctx,
		chromedp.SetValue("ticket-id", *ticketID, chromedp.ByID),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err == nil {
		debugPrintf("request approval: located input#reason in request approval\n")
		err = chromedp.Run(ctx,
			chromedp.Click(`input#reason`),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.KeyEvent(kb.ArrowDown),
			chromedp.KeyEvent(kb.ArrowDown),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Click("div.x-combo-list-item.x-combo-selected"),
			chromedp.KeyEvent(kb.Enter),
		)
		if err != nil {
			log.Fatal(err)
		}
		err = chromedp.Run(ctx,
			chromedp.Click("#ext-gen284"),
		)
		if err != nil {
			log.Fatal(err)
		}
		gotoCyberArk(ctx)
		return false
	}
	return false
}

func selectHost(ctx context.Context, hostname string, ipaddr *string) bool {
	debugPrintf("Go to selectHost\n")

	err := chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 5, chromedp.Tasks{
			chromedp.WaitVisible("/html/body/form[1]/table/tbody/tr/td/table/tbody/tr/td[2]/table[2]/tbody/tr/td/div[2]/div/div/div/div/div/div/div/div/div/div[2]/div[2]/div/div[3]/div/div/div/div/div[1]/div/div[1]/div[2]/div/div[3]/table/tbody/tr/td[7]/div/span"),
		}),
	)

	if err != nil {
		fmt.Printf("%s: host not found\n", hostname)
		return false
	}

	err = chromedp.Run(ctx,
		chromedp.Text("/html/body/form[1]/table/tbody/tr/td/table/tbody/tr/td[2]/table[2]/tbody/tr/td/div[2]/div/div/div/div/div/div/div/div/div/div[2]/div[2]/div/div[3]/div/div/div/div/div[1]/div/div[1]/div[2]/div/div[3]/table/tbody/tr/td[7]/div/span", ipaddr, chromedp.NodeVisible),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = chromedp.Run(ctx,
		chromedp.Click("/html/body/form[1]/table/tbody/tr/td/table/tbody/tr/td[2]/table[2]/tbody/tr/td/div[2]/div/div/div/div/div/div/div/div/div/div[2]/div[2]/div/div[3]/div/div/div/div/div[1]/div/div[1]/div[2]/div/div[3]/table/tbody/tr/td[24]/div/img", chromedp.NodeVisible),
	)
	if err != nil {
		log.Fatal(err)
	}

	debugPrintf("hostname: %s, I.P: %s\n", hostname, *ipaddr)
	return true
}

func searchHost(ctx context.Context, hostname string) bool {
	debugPrintf("Go to searchHost\n")
	err := chromedp.Run(ctx,
		chromedp.WaitVisible("#pvBody_PageTemplate_PVSearch_txtSearch", chromedp.ByID),
		chromedp.Sleep(1*time.Second),
		chromedp.SetValue("#pvBody_PageTemplate_PVSearch_txtSearch", hostname, chromedp.ByID),
		chromedp.SendKeys("#pvBody_PageTemplate_PVSearch_txtSearch", "\n", chromedp.ByID),
	)
	if err != nil {
		return false
	} else {
		return true
	}
}

func gotoFrontPage(ctx context.Context) {
	debugPrintf("Go to front page\n")

	err := chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 15,
			chromedp.Tasks{chromedp.WaitVisible("#ext-gen119", chromedp.ByID)},
		),
	)
	if err != nil {
		fmt.Printf("gotoCyberArk in gotoFrontPage\n")
		gotoCyberArk(ctx)
		return
	}

	// Perform click action on the element
	err = chromedp.Run(ctx,
		chromedp.Click("#ext-gen119"),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func gotoCyberArk(ctx context.Context) {
	debugPrintf("Go to CyberArk\n")

	err := chromedp.Run(ctx,
		chromedp.Navigate(cyberark_url),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func initChromedp() (context.Context, context.CancelFunc) {
	if *browser {
		userInfo()
	}
	// Create a new context
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, _ = chromedp.NewContext(context.Background())

	// Set up options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("headless", !*browser),
		//chromedp.ExecPath("/usr/local/bin/google-chrome-stable"),
		chromedp.ProxyServer("direct://"),
	)

	// Create a new context with the options
	ctx, _ = chromedp.NewExecAllocator(ctx, opts...)

	// Create a new chromedp browser
	ctx, cancel = chromedp.NewContext(ctx)

	// start the browser without a timeout
	if err := chromedp.Run(ctx); err != nil {
		log.Fatal(err)
	}
	return ctx, cancel
}

func loginCyberArk(ctx context.Context) {
	if !*browser {
		userInfo()
	}

	// Navigate to a website
	err := chromedp.Run(ctx,
		chromedp.Navigate(cyberark_url),
		chromedp.WaitVisible("#pvBody_PageTemplate_innerHolder_ctrlLogon_txtUsername", chromedp.ByID),
		chromedp.SendKeys("#pvBody_PageTemplate_innerHolder_ctrlLogon_txtUsername", username, chromedp.ByID),
		chromedp.WaitVisible("#pvBody_PageTemplate_innerHolder_ctrlLogon_txtPassword", chromedp.ByID),
		chromedp.SendKeys("#pvBody_PageTemplate_innerHolder_ctrlLogon_txtPassword", password, chromedp.ByID),
		chromedp.SendKeys("#pvBody_PageTemplate_innerHolder_ctrlLogon_txtPassword", "\n", chromedp.ByID),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Check if login success or failure
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 2,
			chromedp.Tasks{chromedp.WaitVisible("pvBody_PageTemplate_innerHolder_ctrlLogon_lblErrorMessageSummary", chromedp.ByID)},
		),
	)
	if err == nil {
		var loginmsg string
		chromedp.Run(ctx,
			RunWithTimeOut(&ctx, 2,
				chromedp.Tasks{chromedp.Text("pvBody_PageTemplate_innerHolder_ctrlLogon_lblErrorMessage", &loginmsg, chromedp.ByID)}),
		)
		chromedp.Cancel(ctx)
		fmt.Printf("Login failed: %s\n", loginmsg)
		os.Exit(1)
	}

	// must be located the frontpage within 60s, otherwise Exit
	err = chromedp.Run(ctx,
		RunWithTimeOut(&ctx, 60,
			chromedp.Tasks{chromedp.WaitVisible("#ext-gen119", chromedp.ByID)},
		),
	)
	if err != nil {
		chromedp.Cancel(ctx)
		fmt.Printf("Can't find the frontpage within 60s ...\n")
		os.Exit(1)
	}
}

func openHostFile(hostnameFile string) *os.File {
	// Open the file in read-only mode
	file, err := os.Open(hostnameFile)
	if err != nil {
		if os.IsNotExist(err) {
			flag.Usage()
		} else {
			fmt.Fprintf(os.Stderr, "Failed to open file: %#v\n", err)
		}
		os.Exit(1)
	}
	return file
}

func userInfo() {
	cyberuserEnv := os.Getenv("_CYBERUSER")
	cyberpassEnv := os.Getenv("_CYBERPASS")
	cyberurlEnv := os.Getenv("_CYBERURL")

	if cyberurlEnv != "" {
		cyberark_url = cyberurlEnv
		fmt.Printf("Enter Cyberark URL: %s\n", cyberark_url)
	} else {
		fmt.Print("Enter Cyberark URL: ")
		fmt.Scanln(&cyberark_url)
	}

	if cyberuserEnv != "" {
		username = cyberuserEnv
		fmt.Printf("Enter your username: %s\n", username)
	} else {
		fmt.Print("Enter your username: ")
		fmt.Scanln(&username)
	}

	fmt.Printf("Enter your password: ")
	if cyberpassEnv != "" {
		password = cyberpassEnv
	} else {
		// Disable echoing
		oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
		passwordBytes, _ := term.ReadPassword(int(os.Stdin.Fd()))
		password = string(passwordBytes)
		term.Restore(int(os.Stdin.Fd()), oldState)
	}
	fmt.Printf("%s\n", strings.Repeat("*", len(password)))
	//fmt.Printf("Enter your password: %s\n", password)
	fmt.Printf("===>  Open your mobile to tap the Duo  <===\n")
	fmt.Printf("\n")
}
