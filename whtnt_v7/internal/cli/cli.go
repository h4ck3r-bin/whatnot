package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"rnldo.dev/wnshell/internal/engine"
)

const (
	reset = "\033[0m"
	cyan  = "\033[36m"
	pink  = "\033[95m"
	dim   = "\033[2m"
	bold  = "\033[1m"
	green = "\033[32m"
	red   = "\033[31m"
)

type app interface {
	ApproveContinueUnfinishedSessions()
	ApproveStartFreshWithoutUnfinishedSessions()
	CountUnfinishedSessions() (int, error)
	RunTasks(context.Context) error
	RunPreparedSignupCSV(context.Context, string, int) error
	RunSpecificAccountGeneration(context.Context, string, string) (string, error)
	RunPreparedCheckoutCSV(context.Context, string) error
	RunSingleAccountFullCheckout(context.Context, string, bool) error
	RunSingleAccountCheckout(context.Context, engine.SingleAccountCheckoutRequest) error
	RunSessionChecks(context.Context, int) error
	CheckAllOrders(context.Context) ([]engine.OrderListRow, error)
	CheckSessions(context.Context, string) (engine.SessionCheckSummary, error)
	UpdateAccounts(context.Context, string, int) (engine.AccountUpdateSummary, error)
	BuildCreditCheckoutCSV(context.Context, int) (engine.CreditCheckoutCSVSummary, error)
	BuildBulkCheckoutCSV(context.Context, string, string, int) (engine.CreditCheckoutCSVSummary, error)
	BuildBulkReviewCSV(context.Context, string, string, int) (engine.ReviewCSVSummary, error)
	RunBulkReviewCSV(context.Context, string) (engine.ReviewCSVSummary, error)
	ExportTrackingCodes(context.Context, string) (engine.TrackingExportSummary, error)
	ExportOngoingOrders(context.Context, string, string, int) (engine.OngoingOrdersExportSummary, error)
	CheckFriendCodeCredits(context.Context, int) (engine.FriendCodeCreditSummary, error)
	ManageAccountDashboard(context.Context, string, bool) (engine.AccountDashboard, error)
	OpenConversation(context.Context, string, string) (engine.AccountConversation, error)
	SendAccountMessage(context.Context, engine.AccountMessageRequest) error
	ListAccountAddresses(context.Context, string) ([]engine.AccountAddress, error)
	CreateAccountAddress(context.Context, string, engine.AccountAddressInput) (engine.AccountAddress, error)
	UpdateAccountAddress(context.Context, string, engine.AccountAddressInput) (engine.AccountAddress, error)
	DeleteAccountAddress(context.Context, string, string) error
	ListAccountPaymentMethods(context.Context, string) ([]engine.AccountPaymentMethod, error)
	AddAccountPaymentMethod(context.Context, string, engine.AccountPaymentInput) (engine.AccountPaymentMethod, error)
	DeleteAccountPaymentMethod(context.Context, string, string) error
	MarkAccountNotificationsViewed(context.Context, string) error
	AcknowledgeAccountNotification(context.Context, string, string) error
	FollowAccountUser(context.Context, string, string) error
	ListPendingReviews() ([]engine.PendingReviewItem, error)
	SubmitPendingReview(context.Context, string, []int, string) error
	BulkSubmitPendingReviews(context.Context) (engine.BulkReviewSummary, error)
	ListMasters() ([]engine.MasterAccount, error)
	RunMasterCheckout(context.Context, string, engine.MasterCheckoutRequest) error
}

type masterStore interface {
	LoadMasters() ([]engine.MasterAccount, error)
}

func Run(ctx context.Context, app app, _ masterStore) error {
	reader := bufio.NewReader(os.Stdin)
	for {
		engine.SetConsoleTitle("Project Whatnot: Main menu")
		renderHeader()
		fmt.Println("  1. Tasks")
		fmt.Println("  2. Accounts")
		fmt.Println("  3. Account Manager")
		fmt.Println("  4. Testing")
		fmt.Println("  5. Exit")
		fmt.Println()
		choice := prompt(reader, "Choose")

		switch strings.TrimSpace(choice) {
		case "1":
			runTasksMenu(ctx, reader, app)
		case "2":
			runAccountsMenu(ctx, reader, app)
		case "3":
			runAccountManagerEntryMode(ctx, reader, app)
		case "4":
			runTestingMenu(ctx, reader, app)
		case "5":
			return nil
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runTasksMenu(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		engine.SetConsoleTitle("Project Whatnot: Tasks menu")
		renderHeader()
		fmt.Printf("%sTasks%s\n\n", bold, reset)
		fmt.Println("  1. Full Checkout Mode")
		fmt.Println("  2. Aged CSV Mode")
		fmt.Println("  3. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runFullCheckoutMode(ctx, reader, app)
		case "2":
			runAgedCSVMode(ctx, reader, app)
		case "3":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runFullCheckoutMode(ctx context.Context, reader *bufio.Reader, app app) {
	unfinished, err := app.CountUnfinishedSessions()
	if err != nil {
		fmt.Printf("%sRun failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	if unfinished > 0 {
		answer := prompt(reader, fmt.Sprintf("%d sessions unfinished - continue from where they left off? Y/N", unfinished))
		if isYes(answer) {
			app.ApproveContinueUnfinishedSessions()
		} else {
			app.ApproveStartFreshWithoutUnfinishedSessions()
		}
	}
	if err := app.RunTasks(ctx); err != nil {
		var unfinishedErr engine.UnfinishedSessionsError
		if errors.As(err, &unfinishedErr) {
			answer := prompt(reader, fmt.Sprintf("%d sessions unfinished - continue from where they left off? Y/N", unfinishedErr.Count))
			if isYes(answer) {
				app.ApproveContinueUnfinishedSessions()
				err = app.RunTasks(ctx)
			} else {
				app.ApproveStartFreshWithoutUnfinishedSessions()
				err = app.RunTasks(ctx)
			}
		}
		if err == nil {
			fmt.Printf("%sRun completed.%s\n", green, reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sRun failed: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sRun completed.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func runAgedCSVMode(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		renderHeader()
		fmt.Printf("%sAged CSV Mode%s\n\n", bold, reset)
		fmt.Println("  1. Generate CSV")
		fmt.Println("  2. Checkout CSV")
		fmt.Println("  3. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runAgedCSVGenerateMode(ctx, reader, app)
		case "2":
			runAgedCSVCheckoutMode(ctx, reader, app)
		case "3":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runAgedCSVGenerateMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sGenerate Aged CSV%s\n\n", bold, reset)
	fmt.Printf("%sSigns up accounts, checks rewards/status, then appends editable checkout rows to CSV before address/card.%s\n\n", dim, reset)

	rawCount := prompt(reader, "How many to generate")
	count, err := strconv.Atoi(strings.TrimSpace(rawCount))
	if err != nil || count < 1 {
		fmt.Printf("%sEnter a positive generation count.%s\n", red, reset)
		waitForEnter(reader)
		return
	}
	path := chooseStorageCSV(reader, "storage/prepared_tasks.csv", false)
	if err := app.RunPreparedSignupCSV(ctx, path, count); err != nil {
		fmt.Printf("%sAged CSV generation failed: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sAged CSV generation complete.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func runAgedCSVCheckoutMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sCheckout Aged CSV%s\n\n", bold, reset)
	fmt.Printf("%sRefreshes each staged session, then uses the row address/card/products_json to checkout.%s\n\n", dim, reset)

	path := chooseStorageCSV(reader, "storage/prepared_tasks.csv", false)
	if err := app.RunPreparedCheckoutCSV(ctx, path); err != nil {
		fmt.Printf("%sAged CSV checkout failed: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sAged CSV checkout complete.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func runAccountsMenu(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		engine.SetConsoleTitle("Project Whatnot: Accounts menu")
		renderHeader()
		fmt.Printf("%sAccounts%s\n\n", bold, reset)
		fmt.Println("  1. Update All Accounts")
		fmt.Println("  2. Bulk CSV Mode")
		fmt.Println("  3. Check Friend Code Credits")
		fmt.Println("  4. Export Tracking Codes")
		fmt.Println("  5. Export Ongoing Orders")
		fmt.Println("  6. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runUpdateAccountsMode(ctx, reader, app)
		case "2":
			runBulkCSVMode(ctx, reader, app)
		case "3":
			runCheckFriendCodeCreditsMode(ctx, reader, app)
		case "4":
			runExportTrackingCodesMode(ctx, reader, app)
		case "5":
			runExportOngoingOrdersMode(ctx, reader, app)
		case "6":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runUtilityMenu(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		engine.SetConsoleTitle("Project Whatnot: Utility menu")
		renderHeader()
		fmt.Printf("%sUtility%s\n\n", bold, reset)
		fmt.Println("  1. Create One Account")
		fmt.Println("  2. Manage Account")
		fmt.Println("  3. Full Checkout Account")
		fmt.Println("  4. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runSpecificAccountMode(ctx, reader, app)
		case "2":
			runManageAccountMode(ctx, reader, app)
		case "3":
			runSingleAccountFullCheckoutMode(ctx, reader, app)
		case "4":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runTestingMenu(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		engine.SetConsoleTitle("Project Whatnot: Testing menu")
		renderHeader()
		fmt.Printf("%sTesting%s\n\n", bold, reset)
		fmt.Println("  1. SEON Tester")
		fmt.Println("  2. Generate Specific Account")
		fmt.Println("  3. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runSessionCheckMode(ctx, reader, app)
		case "2":
			runSpecificAccountMode(ctx, reader, app)
		case "3":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runSpecificAccountMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sGenerate Specific Account%s\n\n", bold, reset)
	fmt.Printf("%sCreates one account with the supplied email/password, completes generation, then saves a CSV named after the email.%s\n\n", dim, reset)

	email := prompt(reader, "Email")
	password := prompt(reader, "Password")
	path, err := app.RunSpecificAccountGeneration(ctx, email, password)
	if err != nil {
		fmt.Printf("%sSpecific account generation failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sSpecific account generation complete.%s\n", green, reset)
	fmt.Printf("  CSV: %s\n", path)
	waitForEnter(reader)
}

func runSingleAccountCheckoutMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sCheckout Account by Email%s\n\n", bold, reset)
	fmt.Printf("%sUses one stored account/session. Custom details uses the same path as Aged CSV checkout; otherwise it uses stored/default checkout details.%s\n\n", dim, reset)
	runSingleAccountCheckoutPrompt(ctx, reader, app, "")
}

func runSingleAccountFullCheckoutMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sFull Checkout Account%s\n\n", bold, reset)
	fmt.Printf("%sUses one stored account/session with the same checkout decision path as Full Checkout Mode.%s\n\n", dim, reset)
	query := prompt(reader, "Account email/username")
	if err := app.RunSingleAccountFullCheckout(ctx, query, false); err != nil {
		if errors.Is(err, engine.ErrNoBalance) && isYes(prompt(reader, "NO_BALANCE: item costs more than proven credit. Continue anyway? Y/N")) {
			err = app.RunSingleAccountFullCheckout(ctx, query, true)
		}
		if err == nil {
			fmt.Printf("%sFull account checkout completed.%s\n", green, reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sFull account checkout failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sFull account checkout completed.%s\n", green, reset)
	waitForEnter(reader)
}

func runSingleAccountCheckoutPrompt(ctx context.Context, reader *bufio.Reader, app app, query string) {
	if strings.TrimSpace(query) == "" {
		query = prompt(reader, "Account email/username")
	}
	request := engine.SingleAccountCheckoutRequest{Email: query}
	request.UseCustomAddress = isYes(prompt(reader, "Use custom address? Y/N"))
	if request.UseCustomAddress {
		request.AddressLine1 = prompt(reader, "Address line 1")
		request.AddressLine2 = prompt(reader, "Address line 2")
		request.Town = prompt(reader, "Town")
		request.Postcode = prompt(reader, "Postcode")
		request.AddressName = prompt(reader, "Address last name, blank account last name")
	}
	request.UseCustomCard = isYes(prompt(reader, "Use custom card? Y/N"))
	if request.UseCustomCard {
		request.CardNumber = prompt(reader, "Card number")
		request.CardCVC = prompt(reader, "Card CVC")
		request.CardExpMonth = prompt(reader, "Card exp month")
		request.CardExpYear = prompt(reader, "Card exp year")
		request.CardPostcode = prompt(reader, "Card postcode")
	}
	request.UseCustomProducts = isYes(prompt(reader, "Use custom products? Y/N"))
	if request.UseCustomProducts {
		request.RewardLabel = prompt(reader, "Reward label, blank auto")
		request.ProductsJSON = prompt(reader, "Products JSON, blank auto")
	}

	if err := app.RunSingleAccountCheckout(ctx, request); err != nil {
		if errors.Is(err, engine.ErrNoBalance) && isYes(prompt(reader, "NO_BALANCE: item costs more than proven credit. Continue anyway? Y/N")) {
			request.ApproveNoBalance = true
			err = app.RunSingleAccountCheckout(ctx, request)
		}
		if err == nil {
			fmt.Printf("%sSingle account checkout completed.%s\n", green, reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sSingle account checkout failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sSingle account checkout completed.%s\n", green, reset)
	waitForEnter(reader)
}

func runOrderCheckMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sOrder Check Mode%s\n\n", bold, reset)
	fmt.Printf("%sRefreshing live sessions and listing all stored orders for non-complete accounts...%s\n\n", dim, reset)

	rows, err := app.CheckAllOrders(ctx)
	if len(rows) == 0 {
		fmt.Printf("%sNo orders were found for non-complete accounts.%s\n", dim, reset)
	} else {
		printOrderRows(rows)
	}
	if err != nil {
		fmt.Printf("\n%sOrder check completed with warnings: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("\n%sOrder check completed. Sessions and order state are saved.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func runCheckSessionsMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sCheck Sessions%s\n\n", bold, reset)
	fmt.Printf("%sRefreshes stored sessions and marks failed ones as banned/failed so future modes skip them.%s\n\n", dim, reset)

	path := chooseStorageCSV(reader, "", true)
	summary, err := app.CheckSessions(ctx, path)
	if err != nil {
		fmt.Printf("%sSession check failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sSession check complete.%s\n", green, reset)
	fmt.Printf("  Checked: %d\n", summary.Checked)
	fmt.Printf("  Healthy: %d\n", summary.Healthy)
	fmt.Printf("  Banned:  %d\n", summary.Banned)
	fmt.Printf("  Failed:  %d\n", summary.Failed)
	fmt.Printf("  Missing: %d\n", summary.Missing)
	fmt.Printf("  Skipped: %d\n", summary.Skipped)
	fmt.Printf("  Orders:  %d\n", summary.Orders)
	fmt.Printf("  Reviews: %d\n", summary.Reviews)
	if len(summary.Referrers) > 0 {
		fmt.Println()
		fmt.Println("  Ref codes:")
		for _, referrer := range summary.Referrers {
			fmt.Printf("  %-24s %d healthy (%d banned)\n", truncate(referrer.Referrer, 24), referrer.Healthy, referrer.Banned)
		}
	}
	waitForEnter(reader)
}

func runUpdateAccountsMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sUpdate Accounts%s\n\n", bold, reset)
	fmt.Printf("%sRefreshes sessions, updates orders/messages/notifications, and sends discovery webhooks immediately.%s\n\n", dim, reset)

	path := chooseStorageCSV(reader, "", true)
	threads := promptThreadCount(reader)
	summary, err := app.UpdateAccounts(ctx, path, threads)
	if err != nil {
		fmt.Printf("%sAccount update failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sAccount update complete.%s\n", green, reset)
	fmt.Printf("  Checked:       %d\n", summary.Checked)
	fmt.Printf("  Healthy:       %d\n", summary.Healthy)
	fmt.Printf("  Banned:        %d\n", summary.Banned)
	fmt.Printf("  Failed:        %d\n", summary.Failed)
	fmt.Printf("  Missing:       %d\n", summary.Missing)
	fmt.Printf("  Skipped:       %d\n", summary.Skipped)
	fmt.Printf("  Orders:        %d\n", summary.Orders)
	fmt.Printf("  Reviews:       %d\n", summary.PendingReviews)
	fmt.Printf("  Messages:      %d new/unread\n", summary.MessageDiscoveries)
	fmt.Printf("  Notifications: %d new/unread\n", summary.NotificationDiscoveries)
	fmt.Printf("  Webhooks:      %d\n", summary.Webhooks)
	waitForEnter(reader)
}

func runBuildReadyCheckoutCSVMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sBuild Ready Checkout CSV%s\n\n", bold, reset)
	fmt.Printf("%sChecks live credit only, then overwrites storage/all_ready_accounts.csv with accounts above 10 balance.%s\n\n", dim, reset)

	threads := promptThreadCount(reader)
	summary, err := app.BuildCreditCheckoutCSV(ctx, threads)
	if err != nil {
		fmt.Printf("%sReady checkout CSV failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sReady checkout CSV complete.%s\n", green, reset)
	fmt.Printf("  Checked:  %d\n", summary.Checked)
	fmt.Printf("  Eligible: %d\n", summary.Eligible)
	fmt.Printf("  Exported: %d\n", summary.Exported)
	fmt.Printf("  Banned:   %d\n", summary.Banned)
	fmt.Printf("  Failed:   %d\n", summary.Failed)
	fmt.Printf("  Skipped:  %d\n", summary.Skipped)
	fmt.Printf("  CSV:      %s\n", summary.Path)
	waitForEnter(reader)
}

func runBulkCSVMode(ctx context.Context, reader *bufio.Reader, app app) {
	for {
		renderHeader()
		fmt.Printf("%sBulk CSV Mode%s\n\n", bold, reset)
		fmt.Println("  1. Create CSV")
		fmt.Println("  2. Run CSV")
		fmt.Println("  3. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			runBulkCSVCreateMode(ctx, reader, app)
		case "2":
			runBulkCSVRunMode(ctx, reader, app)
		case "3":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runBulkCSVCreateMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sCreate Bulk CSV%s\n\n", bold, reset)
	fmt.Println("  1. Checkout CSV")
	fmt.Println("  2. Review CSV")
	fmt.Println()
	mode := strings.TrimSpace(prompt(reader, "Choose"))
	output := promptStorageOutputPath(reader, "Save file name", "storage/bulk.csv", ".csv")
	fmt.Printf("\n  %sCheck from:%s\n", dim, reset)
	scope := chooseStorageCSV(reader, "", true)
	threads := promptThreadCount(reader)
	switch mode {
	case "1":
		summary, err := app.BuildBulkCheckoutCSV(ctx, output, scope, threads)
		if err != nil {
			fmt.Printf("%sCheckout CSV create failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sCheckout CSV created.%s\n", green, reset)
		fmt.Printf("  Checked:  %d\n", summary.Checked)
		fmt.Printf("  Eligible: %d\n", summary.Eligible)
		fmt.Printf("  Exported: %d\n", summary.Exported)
		fmt.Printf("  Banned:   %d\n", summary.Banned)
		fmt.Printf("  Failed:   %d\n", summary.Failed)
		fmt.Printf("  Skipped:  %d\n", summary.Skipped)
		fmt.Printf("  CSV:      %s\n", summary.Path)
	case "2":
		summary, err := app.BuildBulkReviewCSV(ctx, output, scope, threads)
		if err != nil {
			fmt.Printf("%sReview CSV create failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sReview CSV created.%s\n", green, reset)
		fmt.Printf("  Checked:  %d\n", summary.Checked)
		fmt.Printf("  Exported: %d\n", summary.Exported)
		fmt.Printf("  Failed:   %d\n", summary.Failed)
		fmt.Printf("  Skipped:  %d\n", summary.Skipped)
		fmt.Printf("  CSV:      %s\n", summary.Path)
	default:
		fmt.Printf("%sUnknown choice.%s\n", red, reset)
	}
	waitForEnter(reader)
}

func runBulkCSVRunMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sRun Bulk CSV%s\n\n", bold, reset)
	fmt.Println("  1. Checkout CSV")
	fmt.Println("  2. Review CSV")
	fmt.Println()
	mode := strings.TrimSpace(prompt(reader, "Choose"))
	path := chooseStorageCSV(reader, "storage/bulk.csv", false)
	switch mode {
	case "1":
		if err := app.RunPreparedCheckoutCSV(ctx, path); err != nil {
			fmt.Printf("%sCheckout CSV run failed: %s%s\n", red, err.Error(), reset)
		} else {
			fmt.Printf("%sCheckout CSV run complete.%s\n", green, reset)
		}
	case "2":
		summary, err := app.RunBulkReviewCSV(ctx, path)
		if err != nil {
			fmt.Printf("%sReview CSV run failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			return
		}
		fmt.Printf("%sReview CSV run complete.%s\n", green, reset)
		fmt.Printf("  Submitted: %d\n", summary.Submitted)
		fmt.Printf("  Failed:    %d\n", summary.Failed)
		fmt.Printf("  Skipped:   %d\n", summary.Skipped)
		fmt.Printf("  CSV:       %s\n", summary.Path)
	default:
		fmt.Printf("%sUnknown choice.%s\n", red, reset)
	}
	waitForEnter(reader)
}

func runExportTrackingCodesMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sExport Tracking Codes%s\n\n", bold, reset)
	fmt.Printf("%sWrites tracking codes for active, non-review, non-completed orders to a txt file.%s\n\n", dim, reset)
	path := promptStorageOutputPath(reader, "Save file name", "storage/tracking_codes.txt", ".txt")
	summary, err := app.ExportTrackingCodes(ctx, path)
	if err != nil {
		fmt.Printf("%sTracking export failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sTracking export complete.%s\n", green, reset)
	fmt.Printf("  Exported: %d\n", summary.Exported)
	fmt.Printf("  Skipped:  %d\n", summary.Skipped)
	fmt.Printf("  File:     %s\n", summary.Path)
	waitForEnter(reader)
}

func runExportOngoingOrdersMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sExport Ongoing Orders%s\n\n", bold, reset)
	fmt.Printf("%sRefreshes all accounts first, then writes undelivered orders to CSV. Pending review, delivered, completed, cancelled, refunded, and reviewed orders count as done and are excluded.%s\n\n", dim, reset)
	path := promptStorageOutputPath(reader, "Save file name", "storage/ongoing_orders.csv", ".csv")
	fmt.Printf("\n  %sCheck from:%s\n", dim, reset)
	scope := chooseStorageCSV(reader, "", true)
	threads := promptThreadCount(reader)
	summary, err := app.ExportOngoingOrders(ctx, path, scope, threads)
	if err != nil {
		fmt.Printf("%sOngoing order export failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sOngoing order export complete.%s\n", green, reset)
	fmt.Printf("  Exported: %d\n", summary.Exported)
	fmt.Printf("  Skipped:  %d\n", summary.Skipped)
	fmt.Printf("  CSV:      %s\n", summary.Path)
	if len(summary.Statuses) > 0 {
		fmt.Println()
		fmt.Println("  All checked statuses:")
		for _, status := range summary.Statuses {
			fmt.Printf("  %-28s %d\n", truncate(status.Status, 28), status.Count)
		}
	}
	waitForEnter(reader)
}

func runCheckFriendCodeCreditsMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sCheck Friend Code Credits%s\n\n", bold, reset)
	fmt.Printf("%sRefreshes stored friend-code/master sessions and saves proven live credit balances.%s\n\n", dim, reset)

	threads := promptThreadCount(reader)
	summary, err := app.CheckFriendCodeCredits(ctx, threads)
	if err != nil {
		fmt.Printf("%sFriend code credit check failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sFriend code credit check complete.%s\n", green, reset)
	fmt.Printf("  Checked: %d\n", summary.Checked)
	fmt.Printf("  Healthy: %d\n", summary.Healthy)
	fmt.Printf("  Updated: %d\n", summary.Updated)
	fmt.Printf("  Banned:  %d\n", summary.Banned)
	fmt.Printf("  Failed:  %d\n", summary.Failed)
	fmt.Printf("  Missing: %d\n", summary.Missing)
	fmt.Printf("  Skipped: %d\n", summary.Skipped)
	waitForEnter(reader)
}

func runReviewMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sReview Mode%s\n\n", bold, reset)
	reviews, err := app.ListPendingReviews()
	if err != nil {
		fmt.Printf("%sReview mode failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	if len(reviews) == 0 {
		fmt.Printf("%sNo pending reviews found.%s\n", dim, reset)
		waitForEnter(reader)
		return
	}
	for index, review := range reviews {
		renderHeader()
		fmt.Printf("%sReview Mode%s\n\n", bold, reset)
		fmt.Printf("  %d/%d\n", index+1, len(reviews))
		fmt.Printf("  Account: %s\n", accountDisplay(firstNonEmpty(review.AccountUsername, review.AccountEmail, "-"), review.AccountCredits))
		fmt.Printf("  Order:   %s\n", firstNonEmpty(review.DisplayID, review.ExternalOrderID, review.OrderID))
		if len(review.Items) > 0 {
			fmt.Printf("  Items:   %s\n", strings.Join(review.Items, " | "))
		}
		fmt.Println()
		answer := prompt(reader, "Submit review? Y/N")
		if !isYes(answer) {
			continue
		}
		ratings := []int{
			promptRating(reader, "Question 1 rating"),
			promptRating(reader, "Question 2 rating"),
			promptRating(reader, "Question 3 rating"),
			promptRating(reader, "Question 4 rating"),
		}
		text := prompt(reader, "Review text")
		if err := app.SubmitPendingReview(ctx, review.ID, ratings, text); err != nil {
			fmt.Printf("%sReview submit failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			continue
		}
		fmt.Printf("%sReview submitted.%s\n", green, reset)
		waitForEnter(reader)
	}
}

func runBulkReviewMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sBulk Review Mode%s\n\n", bold, reset)
	fmt.Printf("%sSubmits stored pending reviews using the configured review text chance.%s\n\n", dim, reset)
	answer := prompt(reader, "Submit all pending reviews? Y/N")
	if !isYes(answer) {
		return
	}
	summary, err := app.BulkSubmitPendingReviews(ctx)
	if err != nil {
		fmt.Printf("%sBulk review failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Printf("%sBulk review complete.%s\n", green, reset)
	fmt.Printf("  Submitted: %d\n", summary.Submitted)
	fmt.Printf("  Failed:    %d\n", summary.Failed)
	fmt.Printf("  Skipped:   %d\n", summary.Skipped)
	waitForEnter(reader)
}

func runAccountAddressMenu(ctx context.Context, reader *bufio.Reader, app app, query string) {
	for {
		renderHeader()
		fmt.Printf("%sAddresses%s\n\n", bold, reset)
		addresses, err := app.ListAccountAddresses(ctx, query)
		if err != nil {
			fmt.Printf("%sAddress lookup failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			return
		}
		printAddresses(addresses)
		fmt.Println()
		fmt.Println("  1. Add address")
		fmt.Println("  2. Edit address")
		fmt.Println("  3. Delete address")
		fmt.Println("  4. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			address := promptAddress(reader, engine.AccountAddress{})
			if _, err := app.CreateAccountAddress(ctx, query, address); err != nil {
				fmt.Printf("%sAdd address failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sAddress added.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "2":
			index := promptIndex(reader, "Address number", len(addresses))
			if index < 0 {
				continue
			}
			address := promptAddress(reader, addresses[index])
			address.ID = addresses[index].ID
			if _, err := app.UpdateAccountAddress(ctx, query, address); err != nil {
				fmt.Printf("%sEdit address failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sAddress updated.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "3":
			index := promptIndex(reader, "Address number", len(addresses))
			if index < 0 {
				continue
			}
			if !isYes(prompt(reader, "Delete this address? Y/N")) {
				continue
			}
			if err := app.DeleteAccountAddress(ctx, query, addresses[index].ID); err != nil {
				fmt.Printf("%sDelete address failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sAddress deleted.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "4":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runAccountPaymentMenu(ctx context.Context, reader *bufio.Reader, app app, query string) {
	for {
		renderHeader()
		fmt.Printf("%sPayment Methods%s\n\n", bold, reset)
		methods, err := app.ListAccountPaymentMethods(ctx, query)
		if err != nil {
			fmt.Printf("%sPayment lookup failed: %s%s\n", red, err.Error(), reset)
			waitForEnter(reader)
			return
		}
		printPaymentMethods(methods)
		fmt.Println()
		fmt.Println("  1. Add card")
		fmt.Println("  2. Delete payment method")
		fmt.Println("  3. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			payment := engine.AccountPaymentInput{
				CardNumber: prompt(reader, "Card number"),
				CardCVC:    prompt(reader, "Card CVC"),
				ExpMonth:   prompt(reader, "Exp month"),
				ExpYear:    prompt(reader, "Exp year"),
				Postcode:   prompt(reader, "Billing postcode"),
			}
			if _, err := app.AddAccountPaymentMethod(ctx, query, payment); err != nil {
				fmt.Printf("%sAdd card failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sCard added.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "2":
			index := promptIndex(reader, "Payment number", len(methods))
			if index < 0 {
				continue
			}
			if !isYes(prompt(reader, "Delete this payment method? Y/N")) {
				continue
			}
			if err := app.DeleteAccountPaymentMethod(ctx, query, methods[index].ID); err != nil {
				fmt.Printf("%sDelete payment failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sPayment method deleted.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "3":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func runManageAccountMode(ctx context.Context, reader *bufio.Reader, app app) {
	query := prompt(reader, "Email/username")
	runManageAccountForQuery(ctx, reader, app, query)
}

func runAccountManagerEntryMode(ctx context.Context, reader *bufio.Reader, app app) {
	engine.SetConsoleTitle("Project Whatnot: Account manager")
	renderHeader()
	fmt.Printf("%sAccount Manager%s\n\n", bold, reset)
	query := prompt(reader, "Email/username")
	runManageAccountForQuery(ctx, reader, app, query)
}

func runManageAccountForQuery(ctx context.Context, reader *bufio.Reader, app app, query string) {
	forceRefresh := false
	for {
		dashboard, err := app.ManageAccountDashboard(ctx, query, forceRefresh)
		forceRefresh = false
		renderHeader()
		fmt.Printf("%sManage Account%s\n\n", bold, reset)
		if err != nil {
			fmt.Printf("%sAccount lookup failed: %s%s\n", red, err.Error(), reset)
			if accountLookupCanOfferCreate(err) {
				answer := prompt(reader, "Create this account? 1 yes 2 no")
				if strings.TrimSpace(answer) == "1" || isYes(answer) {
					email := query
					if !strings.Contains(email, "@") {
						email = prompt(reader, "Email")
					}
					password := prompt(reader, "Password")
					path, createErr := app.RunSpecificAccountGeneration(ctx, email, password)
					if createErr != nil {
						fmt.Printf("%sAccount creation failed: %s%s\n", red, createErr.Error(), reset)
						waitForEnter(reader)
						return
					}
					fmt.Printf("%sAccount created.%s\n", green, reset)
					fmt.Printf("  CSV: %s\n", path)
					query = email
					forceRefresh = true
					waitForEnter(reader)
					continue
				}
			}
			waitForEnter(reader)
			return
		}
		printAccountDashboard(dashboard)
		fmt.Println()
		fmt.Println("  1. Refresh account")
		fmt.Println("  2. Open order details")
		fmt.Println("  3. Leave pending review")
		fmt.Println("  4. Open conversation")
		fmt.Println("  5. Reply to conversation")
		fmt.Println("  6. Message seller by username")
		fmt.Println("  7. Message seller about order")
		fmt.Println("  8. Addresses")
		fmt.Println("  9. Payment methods")
		fmt.Println("  10. Read notifications")
		fmt.Println("  11. Follow user by username")
		fmt.Println("  12. Checkout account")
		fmt.Println("  13. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			forceRefresh = true
			continue
		case "2":
			printOrderDetails(reader, dashboard)
		case "3":
			submitDashboardReview(ctx, reader, app, dashboard)
		case "4":
			openDashboardConversation(ctx, reader, app, query, dashboard)
		case "5":
			replyDashboardConversation(ctx, reader, app, query, dashboard)
		case "6":
			username := prompt(reader, "Seller username")
			body := prompt(reader, "Message")
			if err := app.SendAccountMessage(ctx, engine.AccountMessageRequest{Query: query, Username: username, Body: body}); err != nil {
				fmt.Printf("%sMessage failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sMessage sent.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "7":
			order, ok := promptDashboardOrderReference(reader, dashboard)
			if !ok {
				continue
			}
			body := prompt(reader, "Message")
			if err := app.SendAccountMessage(ctx, engine.AccountMessageRequest{Query: query, OrderID: order, Body: body}); err != nil {
				fmt.Printf("%sMessage failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sMessage sent.%s\n", green, reset)
			}
			waitForEnter(reader)
		case "8":
			runAccountAddressMenu(ctx, reader, app, query)
		case "9":
			runAccountPaymentMenu(ctx, reader, app, query)
		case "10":
			if runAccountNotificationsMenu(ctx, reader, app, query, dashboard) {
				forceRefresh = true
			}
		case "11":
			username := prompt(reader, "Username")
			if err := app.FollowAccountUser(ctx, query, username); err != nil {
				fmt.Printf("%sFollow failed: %s%s\n", red, err.Error(), reset)
			} else {
				fmt.Printf("%sFollowed @%s.%s\n", green, strings.TrimPrefix(strings.TrimSpace(username), "@"), reset)
			}
			waitForEnter(reader)
		case "12":
			runSingleAccountCheckoutPrompt(ctx, reader, app, query)
			forceRefresh = true
		case "13":
			return
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func accountLookupCanOfferCreate(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no stored account/session matched") ||
		strings.Contains(text, "stored account not found") ||
		strings.Contains(text, "not found")
}

func promptRating(reader *bufio.Reader, label string) int {
	raw := prompt(reader, label+" (1-5, blank=5)")
	if strings.TrimSpace(raw) == "" {
		return 5
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 5
	}
	if value < 1 {
		return 1
	}
	if value > 5 {
		return 5
	}
	return value
}

func promptThreadCount(reader *bufio.Reader) int {
	raw := strings.TrimSpace(prompt(reader, "Threads, blank=config"))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		fmt.Printf("%sInvalid thread count; using config/default.%s\n", dim, reset)
		return 0
	}
	return value
}

func runSessionCheckMode(ctx context.Context, reader *bufio.Reader, app app) {
	renderHeader()
	fmt.Printf("%sSEON Tester%s\n\n", bold, reset)
	fmt.Printf("%sCreating fresh accounts, running the username refresh checkpoint, then stopping after the requested successful checks...%s\n\n", dim, reset)

	rawIterations := prompt(reader, "Successful iterations")
	iterations, err := strconv.Atoi(strings.TrimSpace(rawIterations))
	if err != nil || iterations < 1 {
		fmt.Printf("%sEnter a positive iteration count.%s\n", red, reset)
		waitForEnter(reader)
		return
	}

	if err := app.RunSessionChecks(ctx, iterations); err != nil {
		fmt.Printf("%sSEON tester stopped: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sSEON tester complete: 0 failed, %d successful.%s\n", green, iterations, reset)
	}
	waitForEnter(reader)
}

func printOrderRows(rows []engine.OrderListRow) {
	fmt.Printf("  %-28s %-42s %-12s %-16s %-18s %-28s\n", "Account", "Item", "Price", "Order date", "Status", "Tracking")
	fmt.Println("  ------------------------------------------------------------------------------------------------------------------------------------------------")
	for _, row := range rows {
		trackingColor := dim
		if strings.TrimSpace(row.Tracking) != "" && row.Tracking != "-" {
			trackingColor = green
		}
		account := accountDisplay(firstNonEmpty(row.AccountUsername, row.AccountEmail, "-"), row.AccountCredits)
		fmt.Printf("  %-28s %-42s %-12s %-16s %-18s %s%-28s%s\n",
			truncate(account, 28),
			truncate(row.Item, 42),
			truncate(row.Price, 12),
			truncate(row.OrderDate, 16),
			truncate(row.Status, 18),
			trackingColor,
			truncate(row.Tracking, 28),
			reset,
		)
	}
}

func printAccountDashboard(dashboard engine.AccountDashboard) {
	account := dashboard.Account
	fmt.Printf("  %s%s%s\n", bold, accountDisplay(firstNonEmpty(account.Username, account.Email, account.ID), dashboard.CreditBalance), reset)
	if account.Email != "" && account.Username != "" {
		fmt.Printf("  %s%s%s\n", dim, account.Email, reset)
	}
	if account.MasterUsernameUsed != "" {
		fmt.Printf("  Referral: %s\n", account.MasterUsernameUsed)
	}
	fmt.Println()
	fmt.Println("  ----------- Orders -------------")
	if len(dashboard.Orders) == 0 {
		fmt.Printf("  %sNo orders found.%s\n", dim, reset)
	} else {
		for index, order := range dashboard.Orders {
			review := ""
			if order.ReviewPending {
				review = " | Review pending"
			}
			tracking := firstNonEmpty(order.Tracking, "-")
			item := "-"
			if len(order.Items) > 0 {
				item = strings.Join(order.Items, " | ")
			}
			display := firstNonEmpty(order.Display, order.ExternalOrderID, order.OrderID, "-")
			fmt.Printf("  %d. %s | %s | @%s | %s | tracking: %s%s\n", index+1, truncate(display, 18), truncate(item, 42), firstNonEmpty(order.SellerUsername, "-"), firstNonEmpty(order.Status, "-"), truncate(tracking, 28), review)
		}
	}
	fmt.Println()
	fmt.Println("  ----------- Messages ------------")
	if len(dashboard.Messages) == 0 {
		fmt.Printf("  %sNo conversations found.%s\n", dim, reset)
	} else {
		for index, message := range dashboard.Messages {
			state := "read"
			color := dim
			if message.Unread {
				state = "unread"
				color = green
			}
			fmt.Printf("  %d. @%s | %s%s%s | %s\n", index+1, firstNonEmpty(message.Username, message.DisplayName, "-"), color, state, reset, truncate(message.Preview, 72))
			if message.LinkedOrder != "" || message.LinkedItem != "" {
				fmt.Printf("     %s%s%s\n", dim, truncate(firstNonEmpty(message.LinkedOrder, message.LinkedItem), 90), reset)
			}
		}
	}
	fmt.Println()
	fmt.Println("  -------- Notifications ----------")
	if len(dashboard.Notifications) == 0 {
		fmt.Printf("  %sNo notifications found.%s\n", dim, reset)
	} else {
		for index, notification := range dashboard.Notifications {
			state := "read"
			color := dim
			if notification.Unread {
				state = "unread"
				color = green
			}
			fmt.Printf("  %d. %s%s%s | %s\n", index+1, color, state, reset, truncate(notification.Message, 84))
		}
	}
	fmt.Println()
	fmt.Println("  -------- Addresses --------------")
	if len(dashboard.Addresses) == 0 {
		fmt.Printf("  %sNo addresses found.%s\n", dim, reset)
	} else {
		for index, address := range dashboard.Addresses {
			flags := ""
			if address.DefaultShipping {
				flags = " | default"
			}
			fmt.Printf("  %d. %s | %s, %s %s%s\n", index+1, firstNonEmpty(address.FullName, "-"), address.Line1, address.City, address.PostalCode, flags)
		}
	}
	fmt.Println()
	fmt.Println("  -------- Payments ---------------")
	if len(dashboard.PaymentMethods) == 0 {
		fmt.Printf("  %sNo payment methods found.%s\n", dim, reset)
	} else {
		for index, method := range dashboard.PaymentMethods {
			flags := ""
			if method.Default {
				flags = " | default"
			}
			fmt.Printf("  %d. %s | %s %s%s\n", index+1, firstNonEmpty(method.CardDescription, method.ID, "-"), firstNonEmpty(method.CardType, "card"), firstNonEmpty(method.Gateway, "-"), flags)
		}
	}
	if len(dashboard.Warnings) > 0 {
		fmt.Println()
		for _, warning := range dashboard.Warnings {
			fmt.Printf("  %sWarning: %s%s\n", red, warning, reset)
		}
	}
}

func printAddresses(addresses []engine.AccountAddress) {
	if len(addresses) == 0 {
		fmt.Printf("  %sNo addresses found.%s\n", dim, reset)
		return
	}
	for index, address := range addresses {
		flags := ""
		if address.DefaultShipping {
			flags = " | default"
		}
		if address.ReturnAddress {
			flags += " | return"
		}
		fmt.Printf("  %d. %s | %s%s\n", index+1, firstNonEmpty(address.FullName, "-"), addressLine(address), flags)
	}
}

func printPaymentMethods(methods []engine.AccountPaymentMethod) {
	if len(methods) == 0 {
		fmt.Printf("  %sNo payment methods found.%s\n", dim, reset)
		return
	}
	for index, method := range methods {
		flags := ""
		if method.Default {
			flags = " | default"
		}
		fmt.Printf("  %d. %s | %s | %s%s\n", index+1, firstNonEmpty(method.CardDescription, method.ID, "-"), firstNonEmpty(method.CardType, "card"), firstNonEmpty(method.Gateway, "-"), flags)
	}
}

func runAccountNotificationsMenu(ctx context.Context, reader *bufio.Reader, app app, query string, dashboard engine.AccountDashboard) bool {
	for {
		renderHeader()
		fmt.Printf("%sRead Notifications%s\n\n", bold, reset)
		fmt.Printf("  %s%s%s\n", bold, accountDisplay(firstNonEmpty(dashboard.Account.Username, dashboard.Account.Email, dashboard.Account.ID), dashboard.CreditBalance), reset)
		if dashboard.Account.Email != "" && dashboard.Account.Username != "" {
			fmt.Printf("  %s%s%s\n", dim, dashboard.Account.Email, reset)
		}
		fmt.Println()
		printNotifications(dashboard.Notifications, true)
		fmt.Println()
		fmt.Println("  1. Open notification")
		fmt.Println("  2. Back")
		fmt.Println()
		switch prompt(reader, "Choose") {
		case "1":
			if len(dashboard.Notifications) == 0 {
				fmt.Printf("%sNo notifications found.%s\n", dim, reset)
				waitForEnter(reader)
				continue
			}
			index := promptIndex(reader, "Notification number", len(dashboard.Notifications))
			if index < 0 {
				continue
			}
			printNotificationDetails(dashboard.Notifications[index])
			waitForEnter(reader)
		case "2":
			return false
		default:
			fmt.Printf("%sUnknown choice.%s\n", red, reset)
			waitForEnter(reader)
		}
	}
}

func printNotifications(notifications []engine.AccountNotification, detailed bool) {
	if len(notifications) == 0 {
		fmt.Printf("  %sNo notifications found.%s\n", dim, reset)
		return
	}
	for index, notification := range notifications {
		state := "read"
		color := dim
		if notification.Unread {
			state = "unread"
			color = green
		}
		fmt.Printf("  %d. %s%s%s | %s\n", index+1, color, state, reset, truncate(notification.Message, 100))
		if !detailed {
			continue
		}
		details := []string{}
		if notification.ActivityType != "" {
			details = append(details, "type: "+notification.ActivityType)
		}
		if notification.PrimaryActor != "" {
			details = append(details, "actor: @"+notification.PrimaryActor)
		}
		if notification.RelatedListing != "" {
			details = append(details, "listing: "+notification.RelatedListing)
		}
		if notification.CreatedAt != "" {
			details = append(details, "created: "+notification.CreatedAt)
		}
		if len(details) > 0 {
			fmt.Printf("     %s%s%s\n", dim, truncate(strings.Join(details, " | "), 110), reset)
		}
		if notification.DeepLink != "" {
			fmt.Printf("     %s%s%s\n", dim, truncate(notification.DeepLink, 110), reset)
		}
	}
}

func printNotificationDetails(notification engine.AccountNotification) {
	state := "read"
	if notification.Unread {
		state = "unread"
	}
	fmt.Println()
	fmt.Printf("  Status:  %s\n", state)
	if notification.CreatedAt != "" {
		fmt.Printf("  Created: %s\n", notification.CreatedAt)
	}
	if notification.ActivityType != "" {
		fmt.Printf("  Type:    %s\n", notification.ActivityType)
	}
	if notification.PrimaryActor != "" {
		fmt.Printf("  Actor:   @%s\n", notification.PrimaryActor)
	}
	if notification.RelatedListing != "" {
		fmt.Printf("  Listing: %s\n", notification.RelatedListing)
	}
	fmt.Println()
	fmt.Printf("  %s\n", notification.Message)
	if notification.DeepLink != "" {
		fmt.Println()
		fmt.Printf("  Link: %s\n", notification.DeepLink)
	}
}

func promptAddress(reader *bufio.Reader, existing engine.AccountAddress) engine.AccountAddressInput {
	return engine.AccountAddressInput{
		FullName:        promptDefault(reader, "Full name", existing.FullName),
		Line1:           promptDefault(reader, "Address line 1", existing.Line1),
		Line2:           promptDefault(reader, "Address line 2", existing.Line2),
		City:            promptDefault(reader, "Town/city", existing.City),
		State:           promptDefault(reader, "State/county", existing.State),
		PostalCode:      promptDefault(reader, "Postcode", existing.PostalCode),
		CountryCode:     strings.ToUpper(firstNonEmpty(promptDefault(reader, "Country code", firstNonEmpty(existing.CountryCode, "GB")), "GB")),
		DefaultShipping: isYes(promptDefault(reader, "Default shipping? Y/N", boolDefault(existing.DefaultShipping))),
		ReturnAddress:   isYes(promptDefault(reader, "Return address? Y/N", boolDefault(existing.ReturnAddress))),
	}
}

func promptDefault(reader *bufio.Reader, label string, fallback string) string {
	if strings.TrimSpace(fallback) == "" {
		return prompt(reader, label)
	}
	value := prompt(reader, fmt.Sprintf("%s [%s]", label, fallback))
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolDefault(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func promptIndex(reader *bufio.Reader, label string, count int) int {
	if count == 0 {
		fmt.Printf("%sNothing to select.%s\n", dim, reset)
		waitForEnter(reader)
		return -1
	}
	raw := prompt(reader, label)
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > count {
		fmt.Printf("%sInvalid selection.%s\n", red, reset)
		waitForEnter(reader)
		return -1
	}
	return index - 1
}

func addressLine(address engine.AccountAddress) string {
	parts := []string{address.Line1, address.Line2, address.City, address.State, address.PostalCode, address.CountryCode}
	out := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, ", ")
}

func printOrderDetails(reader *bufio.Reader, dashboard engine.AccountDashboard) {
	if len(dashboard.Orders) == 0 {
		waitForEnter(reader)
		return
	}
	raw := prompt(reader, "Order number")
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > len(dashboard.Orders) {
		fmt.Printf("%sInvalid order.%s\n", red, reset)
		waitForEnter(reader)
		return
	}
	order := dashboard.Orders[index-1]
	fmt.Println()
	fmt.Printf("  Order:    %s\n", order.Display)
	fmt.Printf("  Seller:   @%s\n", firstNonEmpty(order.SellerUsername, "-"))
	fmt.Printf("  Status:   %s\n", firstNonEmpty(order.Status, "-"))
	fmt.Printf("  Tracking: %s\n", firstNonEmpty(order.Tracking, "-"))
	if order.TrackingURL != "" {
		fmt.Printf("  URL:      %s\n", order.TrackingURL)
	}
	if order.Total != "" {
		fmt.Printf("  Total:    %s\n", order.Total)
	}
	if len(order.Items) > 0 {
		fmt.Printf("  Items:    %s\n", strings.Join(order.Items, " | "))
	}
	if order.ReviewPending {
		fmt.Printf("  Review:   pending\n")
	}
	waitForEnter(reader)
}

func promptDashboardOrderReference(reader *bufio.Reader, dashboard engine.AccountDashboard) (string, bool) {
	raw := prompt(reader, "Order number/display")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		fmt.Printf("%sInvalid order.%s\n", red, reset)
		waitForEnter(reader)
		return "", false
	}
	if index, err := strconv.Atoi(raw); err == nil {
		if index < 1 || index > len(dashboard.Orders) {
			fmt.Printf("%sInvalid order.%s\n", red, reset)
			waitForEnter(reader)
			return "", false
		}
		order := dashboard.Orders[index-1]
		return firstNonEmpty(order.OrderID, order.ExternalOrderID, order.Display), true
	}
	return raw, true
}

func submitDashboardReview(ctx context.Context, reader *bufio.Reader, app app, dashboard engine.AccountDashboard) {
	reviews, err := app.ListPendingReviews()
	if err != nil {
		fmt.Printf("%sReview lookup failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	var accountReviews []engine.PendingReviewItem
	for _, review := range reviews {
		if review.AccountID == dashboard.Account.ID {
			accountReviews = append(accountReviews, review)
		}
	}
	if len(accountReviews) == 0 {
		fmt.Printf("%sNo pending reviews for this account.%s\n", dim, reset)
		waitForEnter(reader)
		return
	}
	for index, review := range accountReviews {
		fmt.Printf("  %d. %s | %s\n", index+1, firstNonEmpty(review.DisplayID, review.ExternalOrderID, review.OrderID), strings.Join(review.Items, " | "))
	}
	raw := prompt(reader, "Review number")
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > len(accountReviews) {
		fmt.Printf("%sInvalid review.%s\n", red, reset)
		waitForEnter(reader)
		return
	}
	text := prompt(reader, "Review text")
	if err := app.SubmitPendingReview(ctx, accountReviews[index-1].ID, []int{5, 5, 5, 5}, text); err != nil {
		fmt.Printf("%sReview submit failed: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sReview submitted.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func openDashboardConversation(ctx context.Context, reader *bufio.Reader, app app, query string, dashboard engine.AccountDashboard) {
	conversationID := chooseConversationID(reader, dashboard)
	if conversationID == "" {
		return
	}
	conversation, err := app.OpenConversation(ctx, query, conversationID)
	if err != nil {
		fmt.Printf("%sConversation failed: %s%s\n", red, err.Error(), reset)
		waitForEnter(reader)
		return
	}
	fmt.Println()
	if conversation.LinkedOrder != "" || conversation.LinkedItem != "" {
		fmt.Printf("  %s%s%s\n\n", dim, firstNonEmpty(conversation.LinkedOrder, conversation.LinkedItem), reset)
	}
	for _, message := range conversation.Messages {
		from := firstNonEmpty(message.From, "-")
		if message.OwnMessage {
			from = "You"
		}
		fmt.Printf("  %s: %s\n", from, message.Body)
	}
	waitForEnter(reader)
}

func replyDashboardConversation(ctx context.Context, reader *bufio.Reader, app app, query string, dashboard engine.AccountDashboard) {
	conversationID := chooseConversationID(reader, dashboard)
	if conversationID == "" {
		return
	}
	body := prompt(reader, "Message")
	if err := app.SendAccountMessage(ctx, engine.AccountMessageRequest{Query: query, ConversationID: conversationID, Body: body}); err != nil {
		fmt.Printf("%sReply failed: %s%s\n", red, err.Error(), reset)
	} else {
		fmt.Printf("%sReply sent.%s\n", green, reset)
	}
	waitForEnter(reader)
}

func chooseConversationID(reader *bufio.Reader, dashboard engine.AccountDashboard) string {
	if len(dashboard.Messages) == 0 {
		fmt.Printf("%sNo conversations found.%s\n", dim, reset)
		waitForEnter(reader)
		return ""
	}
	raw := prompt(reader, "Conversation number")
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > len(dashboard.Messages) {
		fmt.Printf("%sInvalid conversation.%s\n", red, reset)
		waitForEnter(reader)
		return ""
	}
	return dashboard.Messages[index-1].ConversationID
}

func runMasterMode(ctx context.Context, reader *bufio.Reader, app app) error {
	masters, err := app.ListMasters()
	if err != nil {
		return err
	}

	renderHeader()
	fmt.Printf("%sMaster Accounts%s\n\n", bold, reset)
	if len(masters) == 0 {
		fmt.Printf("%sNo masters are stored yet. Seed storage/masters.json or let candidates complete their lifecycle.%s\n", dim, reset)
		waitForEnter(reader)
		return nil
	}

	fmt.Printf("  %-4s %-24s %-32s %-16s %-10s %-10s\n", "#", "Username", "Email", "State", "Signups", "Cap")
	fmt.Println("  ------------------------------------------------------------------------------------------------")
	for i, master := range masters {
		stateColor := ""
		if master.State == engine.MasterStateReady {
			stateColor = green
		} else if master.State == engine.MasterStateFailed || master.State == engine.MasterStateDormant {
			stateColor = red
		}
		fmt.Printf("  %-4d %-24s %-32s %s%-16s%s %-10d %-10d\n",
			i+1,
			truncate(master.Username, 24),
			truncate(master.Email, 32),
			stateColor,
			master.State,
			reset,
			master.SignupCount,
			master.SignupCap,
		)
	}
	fmt.Println()

	raw := prompt(reader, "Ready Master number")
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > len(masters) {
		return fmt.Errorf("invalid master number")
	}
	selected := masters[index-1]
	if selected.State != engine.MasterStateReady {
		return fmt.Errorf("selected master %s is %s, not Ready", selected.Username, selected.State)
	}

	fmt.Println()
	fmt.Printf("%sCheckout details for %s%s\n", bold, selected.Username, reset)
	request := engine.MasterCheckoutRequest{
		ProductIdentifier: prompt(reader, "Product/listing identifier"),
		ProductID:         prompt(reader, "Product ID, if required"),
		ListingID:         prompt(reader, "Listing ID, if required"),
		Price:             prompt(reader, "Price"),
		CardNumber:        prompt(reader, "Card number"),
		CardCVC:           prompt(reader, "Card CVC"),
		CardExpMonth:      prompt(reader, "Card exp month"),
		CardExpYear:       prompt(reader, "Card exp year"),
		CardPostcode:      prompt(reader, "Card postcode"),
		ExtraFields:       prompt(reader, "Any extra checkout fields as JSON/text"),
	}

	if err := app.RunMasterCheckout(ctx, selected.ID, request); err != nil {
		return err
	}
	fmt.Printf("%sMaster checkout completed. %s is now Dormant.%s\n", green, selected.Username, reset)
	waitForEnter(reader)
	return nil
}

func renderHeader() {
	fmt.Print("\033[H\033[2J")
	fmt.Println()
	fmt.Printf("  %s%sProject Whatnot%s\n", bold, cyan, reset)
	fmt.Printf("  %sWhatnot Referral Bot%s\n", dim, reset)
	fmt.Printf("  %s------------------------------------------------------%s\n\n", pink, reset)
}

func prompt(reader *bufio.Reader, label string) string {
	fmt.Printf("  %s:%s ", label, reset)
	value, _ := reader.ReadString('\n')
	return strings.TrimSpace(value)
}

func chooseStorageCSV(reader *bufio.Reader, defaultPath string, allowAllSessions bool) string {
	options := storageCSVOptions()
	fmt.Println()
	if allowAllSessions {
		fmt.Printf("  %s0. All sessions%s\n", dim, reset)
	} else if strings.TrimSpace(defaultPath) != "" {
		fmt.Printf("  %s0. %s%s\n", dim, defaultPath, reset)
	}
	for index, path := range options {
		fmt.Printf("  %d. %s\n", index+1, path)
	}
	fmt.Println()
	choice := prompt(reader, "CSV")
	if strings.TrimSpace(choice) == "" || strings.TrimSpace(choice) == "0" {
		return defaultPath
	}
	index, err := strconv.Atoi(strings.TrimSpace(choice))
	if err == nil && index >= 1 && index <= len(options) {
		return options[index-1]
	}
	return defaultPath
}

func promptStorageOutputPath(reader *bufio.Reader, label string, defaultPath string, ext string) string {
	raw := strings.TrimSpace(prompt(reader, label))
	if raw == "" {
		return filepath.ToSlash(filepath.Clean(defaultPath))
	}
	ext = strings.TrimSpace(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	path := raw
	if !strings.ContainsAny(path, `/\`) {
		path = filepath.Join("storage", path)
	}
	if ext != "" && !strings.EqualFold(filepath.Ext(path), ext) {
		if filepath.Ext(path) == "" {
			path += ext
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func storageCSVOptions() []string {
	var options []string
	seen := map[string]bool{}
	add := func(path string) {
		path = filepath.ToSlash(filepath.Clean(path))
		if strings.TrimSpace(path) == "" || seen[strings.ToLower(path)] {
			return
		}
		seen[strings.ToLower(path)] = true
		options = append(options, path)
	}
	_ = filepath.WalkDir("storage", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".csv") {
			add(path)
		}
		return nil
	})
	add(filepath.Join("storage", "prepared_tasks.csv"))
	return options
}

func waitForEnter(reader *bufio.Reader) {
	fmt.Printf("\n  %sPress Enter to continue.%s", dim, reset)
	_, _ = reader.ReadString('\n')
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func accountDisplay(label string, credits string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "-"
	}
	credits = strings.TrimSpace(credits)
	if credits == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, credits)
}
