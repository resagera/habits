package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"streaks-backend/internal/aicoder"
	"streaks-backend/internal/auth"
	"streaks-backend/internal/egress"
	"streaks-backend/internal/extension"
	"streaks-backend/internal/files"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/rates"
	"streaks-backend/internal/receiptimport"
	"streaks-backend/internal/store"
	"streaks-backend/internal/terminal"
	"streaks-backend/internal/tvrelay"
)

// MailInfo — что странице «Почта» нужно знать о приёмнике: на каком имени он
// представляется и для каких доменов принимает.
type MailInfo struct {
	Hostname string
	Domains  []string
	// Unblock снимает блокировку и в памяти приёмника: без этого кнопка
	// «разблокировать» чистила бы только базу и работала бы до перезапуска
	Unblock func(ctx context.Context, ip string) error
	// Receipts — разбор писем магазинов в траты Finance
	Receipts *receiptimport.Importer
}

func New(st *store.Store, authMW *auth.Middleware, bot *notify.Bot, egressHub *egress.Hub, aiHub *aicoder.Hub, exts *extension.Archives, ratesCache *rates.Cache, logger *slog.Logger, devCORS bool, staticDir, dataDir string, mailInfo MailInfo) http.Handler {
	tracker := &trackerHandlers{store: st, bot: bot}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         user.ID,
			"username":   user.Username,
			"first_name": user.FirstName,
			"is_admin":   authMW.IsAdminSession(r.Context()),
		})
	})
	api.HandleFunc("GET /api/v1/tracker/categories", tracker.listCategories)
	api.HandleFunc("POST /api/v1/tracker/categories", tracker.createCategory)
	api.HandleFunc("PATCH /api/v1/tracker/categories/{id}", tracker.updateCategory)
	api.HandleFunc("DELETE /api/v1/tracker/categories/{id}", tracker.deleteCategory)
	api.HandleFunc("GET /api/v1/tracker/marks", tracker.marks)
	api.HandleFunc("POST /api/v1/tracker/marks/toggle", tracker.toggleMark)
	api.HandleFunc("POST /api/v1/tracker/marks/increment", tracker.incrementMark)
	api.HandleFunc("GET /api/v1/tracker/categories/{id}/history", tracker.history)
	api.HandleFunc("POST /api/v1/tracker/categories/{id}/share", tracker.share)
	api.HandleFunc("GET /api/v1/tracker/categories/{id}/shares", tracker.listShares)
	api.HandleFunc("DELETE /api/v1/tracker/categories/{id}/shares/{userID}", tracker.revokeShare)

	tasks := &tasksHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/tasks", tasks.list)
	api.HandleFunc("POST /api/v1/tasks", tasks.create)
	api.HandleFunc("GET /api/v1/tasks/done", tasks.listDone)
	api.HandleFunc("GET /api/v1/tasks/summary", tasks.summary)
	api.HandleFunc("GET /api/v1/tasks/{id}", tasks.get)
	api.HandleFunc("PATCH /api/v1/tasks/{id}", tasks.update)
	api.HandleFunc("DELETE /api/v1/tasks/{id}", tasks.delete)
	api.HandleFunc("POST /api/v1/tasks/{id}/complete", tasks.complete)
	api.HandleFunc("POST /api/v1/tasks/{id}/checklist", tasks.addChecklistItem)
	api.HandleFunc("PATCH /api/v1/tasks/checklist/{id}", tasks.updateChecklistItem)
	api.HandleFunc("DELETE /api/v1/tasks/checklist/{id}", tasks.deleteChecklistItem)
	api.HandleFunc("POST /api/v1/tasks/projects", tasks.createProject)
	api.HandleFunc("PATCH /api/v1/tasks/projects/{id}", tasks.updateProject)
	api.HandleFunc("DELETE /api/v1/tasks/projects/{id}", tasks.deleteProject)
	api.HandleFunc("POST /api/v1/tasks/projects/{id}/share", tasks.shareProject)
	api.HandleFunc("GET /api/v1/tasks/projects/{id}/shares", tasks.listProjectShares)
	api.HandleFunc("DELETE /api/v1/tasks/projects/{id}/shares/{userID}", tasks.revokeProjectShare)

	access := &accessHandlers{store: st, authMW: authMW}
	api.HandleFunc("GET /api/v1/me/pages", access.mePages)
	api.HandleFunc("GET /api/v1/me/pages/order", access.pageOrder)
	api.HandleFunc("PUT /api/v1/me/pages/pinned", access.setMenuPinned)
	api.HandleFunc("PUT /api/v1/me/pages/hidden", access.setMenuHidden)
	api.HandleFunc("GET /api/v1/search", access.search)
	api.HandleFunc("GET /api/v1/share/recipients", access.recentRecipients)

	var adminIDs []int64
	for id := range authMW.AdminIDs {
		adminIDs = append(adminIDs, id)
	}
	help := &helpHandlers{store: st, bot: bot, adminIDs: adminIDs}
	api.HandleFunc("POST /api/v1/help/contact", help.contact)

	srvH := &serversHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/servers", srvH.list)
	api.HandleFunc("POST /api/v1/servers", srvH.create)
	api.HandleFunc("PUT /api/v1/servers/{id}", srvH.update)
	api.HandleFunc("DELETE /api/v1/servers/{id}", srvH.delete)
	api.HandleFunc("GET /api/v1/servers/{id}/history", srvH.history)
	api.HandleFunc("POST /api/v1/servers/{id}/refresh", srvH.refresh)

	filesHub := files.NewHub()
	fh := &filesHandlers{store: st, hub: filesHub, tickets: files.NewTickets()}
	api.HandleFunc("GET /api/v1/files/machines", fh.list)
	api.HandleFunc("POST /api/v1/files/machines", fh.create)
	api.HandleFunc("PATCH /api/v1/files/machines/{id}", fh.rename)
	api.HandleFunc("DELETE /api/v1/files/machines/{id}", fh.delete)
	api.HandleFunc("GET /api/v1/files/machines/{id}/list", fh.listDir)
	api.HandleFunc("POST /api/v1/files/machines/{id}/mkdir", fh.op("mkdir"))
	api.HandleFunc("POST /api/v1/files/machines/{id}/rename", fh.op("rename"))
	api.HandleFunc("POST /api/v1/files/machines/{id}/remove", fh.op("remove"))
	api.HandleFunc("POST /api/v1/files/machines/{id}/upload", fh.upload)
	api.HandleFunc("POST /api/v1/files/machines/{id}/ticket", fh.ticket)

	ai := &aiHandlers{store: st, hub: aiHub, bot: bot, logger: logger}
	// результаты прогонов приходят асинхронно по WS агента — вне HTTP-запроса
	aiHub.OnRunStatus = func(machineID, runID int64, status string) {
		if status == "running" {
			if err := st.MarkAIRunRunning(context.Background(), runID); err != nil {
				logger.Error("ai: mark running", "run_id", runID, "error", err)
			}
		}
	}
	aiHub.OnRunResult = func(machineID int64, f aicoder.Frame) error {
		updated, err := st.FinishAIRun(context.Background(), f.RunID, f.OK, f.Output, f.Error, f.Meta, f.SessionID)
		if err != nil {
			logger.Error("ai: finish run", "run_id", f.RunID, "error", err)
			return err
		}
		if updated { // повторная доставка не дублирует уведомление
			ai.notifyRunFinished(f.RunID, f.OK, f.Output, f.Error)
		}
		return nil
	}
	// агент подключился — дослать прогоны, скопившиеся в очереди (офлайн)
	aiHub.OnConnect = func(machineID int64) {
		ai.deliverQueued(machineID)
	}
	// живой лог прогона — построчный append
	aiHub.OnRunLog = func(machineID, runID int64, line string) {
		if err := st.AppendAIRunLog(context.Background(), runID, line); err != nil {
			logger.Error("ai: append log", "run_id", runID, "error", err)
		}
	}
	api.HandleFunc("GET /api/v1/ai/machines", ai.listMachines)
	api.HandleFunc("POST /api/v1/ai/machines", ai.createMachine)
	api.HandleFunc("PATCH /api/v1/ai/machines/{id}", ai.renameMachine)
	api.HandleFunc("DELETE /api/v1/ai/machines/{id}", ai.deleteMachine)
	api.HandleFunc("POST /api/v1/ai/machines/{id}/check", ai.checkTool)
	api.HandleFunc("POST /api/v1/ai/machines/{id}/usage", ai.toolUsage)
	api.HandleFunc("POST /api/v1/ai/machines/{id}/sessions", ai.listSessions)
	api.HandleFunc("POST /api/v1/ai/machines/{id}/session", ai.getSession)
	api.HandleFunc("GET /api/v1/ai/tasks", ai.listTasks)
	api.HandleFunc("POST /api/v1/ai/tasks", ai.createTask)
	api.HandleFunc("GET /api/v1/ai/tasks/{id}", ai.getTask)
	api.HandleFunc("POST /api/v1/ai/tasks/{id}/runs", ai.continueTask)
	api.HandleFunc("DELETE /api/v1/ai/tasks/{id}", ai.deleteTask)
	api.HandleFunc("POST /api/v1/ai/runs/{id}/cancel", ai.cancelRun)
	api.HandleFunc("GET /api/v1/ai/schedules", ai.listSchedules)
	api.HandleFunc("POST /api/v1/ai/schedules", ai.createSchedule)
	api.HandleFunc("PATCH /api/v1/ai/schedules/{id}", ai.updateSchedule)
	api.HandleFunc("DELETE /api/v1/ai/schedules/{id}", ai.deleteSchedule)

	termHub := terminal.NewHub()
	th := &terminalHandlers{store: st, hub: termHub, tickets: terminal.NewTickets()}
	tvh := &tvHandlers{store: st, hub: tvrelay.NewHub(), tickets: terminal.NewTickets()}
	api.HandleFunc("GET /api/v1/terminal/machines", th.list)
	api.HandleFunc("POST /api/v1/terminal/machines", th.create)
	api.HandleFunc("PATCH /api/v1/terminal/machines/{id}", th.rename)
	api.HandleFunc("DELETE /api/v1/terminal/machines/{id}", th.delete)
	api.HandleFunc("POST /api/v1/terminal/machines/{id}/session", th.session)

	tpl := &checkerTemplatesHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/checker/templates", tpl.list)
	api.HandleFunc("POST /api/v1/checker/templates", tpl.save)
	api.HandleFunc("PUT /api/v1/checker/templates/{id}", tpl.save)
	api.HandleFunc("DELETE /api/v1/checker/templates/{id}", tpl.delete)
	api.HandleFunc("POST /api/v1/checker/templates/{id}/start", tpl.start)
	api.HandleFunc("POST /api/v1/checker/templates/{id}/share-token", tpl.shareToken)
	api.HandleFunc("POST /api/v1/checker/templates/{id}/send", tpl.send)
	api.HandleFunc("POST /api/v1/checker/templates/redeem", tpl.redeem)

	checker := &checkerHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/checker/groups", checker.listGroups)
	api.HandleFunc("POST /api/v1/checker/groups", checker.createGroup)
	api.HandleFunc("PATCH /api/v1/checker/groups/{id}", checker.updateGroup)
	api.HandleFunc("DELETE /api/v1/checker/groups/{id}", checker.deleteGroup)
	api.HandleFunc("POST /api/v1/checker/groups/reorder", checker.reorderGroups)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/move", checker.moveGroup)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/duplicate", checker.duplicateGroup)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/restore", checker.restoreGroup)
	api.HandleFunc("GET /api/v1/checker/trash", checker.listTrash)
	api.HandleFunc("DELETE /api/v1/checker/trash", checker.emptyTrash)
	api.HandleFunc("DELETE /api/v1/checker/trash/{id}", checker.purgeGroup)
	api.HandleFunc("PUT /api/v1/checker/trash-days", checker.setTrashDays)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/items", checker.createItem)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/items/bulk", checker.bulkItems)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/share-token", checker.shareGroupToken)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/send", checker.sendGroup)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/share-access", checker.shareAccess)
	api.HandleFunc("GET /api/v1/checker/groups/{id}/history", checker.history)
	api.HandleFunc("PATCH /api/v1/checker/groups/{id}/recurring", checker.setRecurring)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/reminder", checker.setGroupReminder)
	api.HandleFunc("POST /api/v1/checker/items/{id}/reminder", checker.setItemReminder)
	api.HandleFunc("POST /api/v1/checker/groups/{id}/reset", checker.resetNow)
	api.HandleFunc("GET /api/v1/checker/groups/{id}/snapshots", checker.listSnapshots)
	api.HandleFunc("GET /api/v1/checker/groups/{id}/snapshots/{day}", checker.getSnapshot)
	api.HandleFunc("GET /api/v1/checker/groups/{id}/shares", checker.listShares)
	api.HandleFunc("DELETE /api/v1/checker/groups/{id}/shares/{userID}", checker.revokeShare)
	api.HandleFunc("DELETE /api/v1/checker/shared/{id}", checker.leaveShared)
	api.HandleFunc("POST /api/v1/checker/groups/redeem", checker.redeemGroup)
	api.HandleFunc("POST /api/v1/checker/groups/import", checker.importGroup)

	projects := &projectsHandlers{store: st, bot: bot, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/projects", projects.list)
	api.HandleFunc("POST /api/v1/projects", projects.create)
	api.HandleFunc("POST /api/v1/projects/categories", projects.createCategory)
	api.HandleFunc("PATCH /api/v1/projects/categories/{id}", projects.updateCategory)
	api.HandleFunc("DELETE /api/v1/projects/categories/{id}", projects.deleteCategory)
	api.HandleFunc("GET /api/v1/projects/{id}", projects.get)
	api.HandleFunc("PATCH /api/v1/projects/{id}", projects.update)
	api.HandleFunc("DELETE /api/v1/projects/{id}", projects.delete)
	api.HandleFunc("GET /api/v1/projects/{id}/history", projects.history)
	api.HandleFunc("POST /api/v1/projects/{id}/blocks", projects.createBlock)
	api.HandleFunc("PATCH /api/v1/projects/blocks/{id}", projects.updateBlock)
	api.HandleFunc("DELETE /api/v1/projects/blocks/{id}", projects.deleteBlock)
	api.HandleFunc("POST /api/v1/projects/{id}/upload", projects.upload)
	api.HandleFunc("POST /api/v1/projects/{id}/share", projects.share)
	api.HandleFunc("GET /api/v1/projects/{id}/shares", projects.listShares)
	api.HandleFunc("DELETE /api/v1/projects/{id}/shares/{userId}", projects.revokeShare)

	food := &foodHandlers{store: st, bot: bot, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/food/profile", food.getProfile)
	api.HandleFunc("PUT /api/v1/food/profile", food.putProfile)
	api.HandleFunc("POST /api/v1/food/profile/calculate", food.calculate)
	api.HandleFunc("GET /api/v1/food/goals", food.listGoals)
	api.HandleFunc("POST /api/v1/food/goals", food.createGoal)
	api.HandleFunc("GET /api/v1/food/products", food.listProducts)
	api.HandleFunc("POST /api/v1/food/products", food.createProduct)
	api.HandleFunc("GET /api/v1/food/products/{id}", food.getProduct)
	api.HandleFunc("PUT /api/v1/food/products/{id}", food.updateProduct)
	api.HandleFunc("DELETE /api/v1/food/products/{id}", food.deleteProduct)
	api.HandleFunc("GET /api/v1/food/diary", food.diary)
	api.HandleFunc("POST /api/v1/food/meals", food.createMeal)
	api.HandleFunc("GET /api/v1/food/meals/{id}", food.getMeal)
	api.HandleFunc("PUT /api/v1/food/meals/{id}", food.updateMeal)
	api.HandleFunc("DELETE /api/v1/food/meals/{id}", food.deleteMeal)
	api.HandleFunc("POST /api/v1/food/meals/{id}/duplicate", food.duplicateMeal)
	api.HandleFunc("POST /api/v1/food/meals/{id}/save-as-template", food.mealToTemplate)
	api.HandleFunc("GET /api/v1/food/templates", food.listTemplates)
	api.HandleFunc("POST /api/v1/food/templates", food.createTemplate)
	api.HandleFunc("PUT /api/v1/food/templates/{id}", food.updateTemplate)
	api.HandleFunc("DELETE /api/v1/food/templates/{id}", food.deleteTemplate)
	api.HandleFunc("POST /api/v1/food/templates/{id}/create-meal", food.templateToMeal)
	api.HandleFunc("GET /api/v1/food/recipes", food.listRecipes)
	api.HandleFunc("POST /api/v1/food/recipes", food.createRecipe)
	api.HandleFunc("PUT /api/v1/food/recipes/{id}", food.updateRecipe)
	api.HandleFunc("DELETE /api/v1/food/recipes/{id}", food.deleteRecipe)
	api.HandleFunc("POST /api/v1/food/recipes/{id}/create-meal", food.recipeToMeal)
	api.HandleFunc("POST /api/v1/food/shares", food.createShare)
	api.HandleFunc("GET /api/v1/food/shares", food.listShares)
	api.HandleFunc("PATCH /api/v1/food/shares/{userId}", food.updateShare)
	api.HandleFunc("DELETE /api/v1/food/shares/{userId}", food.revokeShare)
	api.HandleFunc("GET /api/v1/food/shared", food.listSharedWithMe)
	api.HandleFunc("DELETE /api/v1/food/shared/{ownerId}", food.leaveShared)
	api.HandleFunc("GET /api/v1/food/shared/{ownerId}/diary", food.sharedDiary)
	api.HandleFunc("GET /api/v1/food/stats", food.stats)
	api.HandleFunc("GET /api/v1/food/metrics/{key}", food.metricSeries)
	api.HandleFunc("POST /api/v1/food/upload", food.upload)

	foodPlan := &foodPlanHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/food/plans", foodPlan.list)
	api.HandleFunc("POST /api/v1/food/plans", foodPlan.create)
	api.HandleFunc("POST /api/v1/food/plans/redeem", foodPlan.redeem)
	api.HandleFunc("GET /api/v1/food/plans/today", foodPlan.today)
	api.HandleFunc("GET /api/v1/food/plans/{id}", foodPlan.get)
	api.HandleFunc("PUT /api/v1/food/plans/{id}", foodPlan.update)
	api.HandleFunc("DELETE /api/v1/food/plans/{id}", foodPlan.delete)
	api.HandleFunc("POST /api/v1/food/plans/{id}/duplicate", foodPlan.duplicate)
	api.HandleFunc("POST /api/v1/food/plans/{id}/copy-days", foodPlan.copyDays)
	api.HandleFunc("POST /api/v1/food/plans/{id}/participants", foodPlan.createParticipant)
	api.HandleFunc("PUT /api/v1/food/plans/{id}/participants/{pid}", foodPlan.updateParticipant)
	api.HandleFunc("DELETE /api/v1/food/plans/{id}/participants/{pid}", foodPlan.deleteParticipant)
	api.HandleFunc("POST /api/v1/food/plans/{id}/slots", foodPlan.createSlot)
	api.HandleFunc("PUT /api/v1/food/plans/{id}/slots/{sid}", foodPlan.updateSlot)
	api.HandleFunc("DELETE /api/v1/food/plans/{id}/slots/{sid}", foodPlan.deleteSlot)
	api.HandleFunc("POST /api/v1/food/plans/{id}/apply", foodPlan.apply)
	api.HandleFunc("POST /api/v1/food/plans/{id}/share", foodPlan.share)
	api.HandleFunc("GET /api/v1/food/plans/{id}/shares", foodPlan.listShares)
	api.HandleFunc("PATCH /api/v1/food/plans/{id}/shares/{userId}", foodPlan.updateShare)
	api.HandleFunc("DELETE /api/v1/food/plans/{id}/shares/{userId}", foodPlan.revokeShare)
	api.HandleFunc("DELETE /api/v1/food/plans/{id}/leave", foodPlan.leave)
	api.HandleFunc("POST /api/v1/food/plans/{id}/share-link", foodPlan.shareLink)

	autom := &automationHandlers{store: st, bot: bot, egress: egressHub}
	api.HandleFunc("GET /api/v1/automation", autom.list)
	api.HandleFunc("POST /api/v1/automation", autom.create)
	api.HandleFunc("PATCH /api/v1/automation/{id}", autom.update)
	api.HandleFunc("DELETE /api/v1/automation/{id}", autom.delete)
	api.HandleFunc("GET /api/v1/automation/{id}/runs", autom.runs)
	api.HandleFunc("POST /api/v1/automation/{id}/run", autom.run)
	api.HandleFunc("GET /api/v1/automation/agent-info", autom.agentInfo)
	api.HandleFunc("POST /api/v1/automation/agent-token/regenerate", autom.regenAgentToken)

	contacts := &contactsHandlers{store: st, bot: bot, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/contacts", contacts.list)
	api.HandleFunc("POST /api/v1/contacts", contacts.create)
	api.HandleFunc("PATCH /api/v1/contacts/{id}", contacts.update)
	api.HandleFunc("DELETE /api/v1/contacts/{id}", contacts.delete)
	api.HandleFunc("POST /api/v1/contacts/{id}/photos", contacts.uploadPhoto)
	api.HandleFunc("DELETE /api/v1/contacts/{id}/photos/{photoId}", contacts.deletePhoto)
	api.HandleFunc("GET /api/v1/contacts/incoming", contacts.incoming)
	api.HandleFunc("POST /api/v1/contacts/incoming/{id}/accept", contacts.accept)
	api.HandleFunc("POST /api/v1/contacts/incoming/{id}/decline", contacts.decline)
	api.HandleFunc("PATCH /api/v1/checker/items/{id}", checker.updateItem)
	api.HandleFunc("DELETE /api/v1/checker/items/{id}", checker.deleteItem)

	cal := &calendarHandlers{store: st}
	api.HandleFunc("GET /api/v1/calendar", cal.data)
	api.HandleFunc("GET /api/v1/calendar/prefs", cal.getPrefs)
	api.HandleFunc("PUT /api/v1/calendar/prefs", cal.setPrefs)

	tests := &testsHandlers{store: st, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/tests/decks", tests.listDecks)
	api.HandleFunc("GET /api/v1/tests/decks/{id}/groups", tests.listGroups)
	api.HandleFunc("POST /api/v1/tests/decks/{id}/reset", tests.reset)
	api.HandleFunc("POST /api/v1/tests/sessions", tests.startSession)
	api.HandleFunc("GET /api/v1/tests/sessions/{id}", tests.getSession)
	api.HandleFunc("POST /api/v1/tests/sessions/{id}/answer", tests.answer)
	api.HandleFunc("POST /api/v1/tests/sessions/{id}/finish", tests.finishSession)
	api.HandleFunc("GET /api/v1/tests/sessions/{id}/review", tests.review)
	api.HandleFunc("GET /api/v1/tests/settings", tests.getSettings)
	api.HandleFunc("PUT /api/v1/tests/settings", tests.setSettings)

	appear := &appearanceHandlers{store: st, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/appearance", appear.get)
	api.HandleFunc("PUT /api/v1/appearance", appear.set)
	api.HandleFunc("POST /api/v1/appearance/themes", appear.saveTheme)
	api.HandleFunc("DELETE /api/v1/appearance/themes/{id}", appear.deleteTheme)
	api.HandleFunc("POST /api/v1/appearance/themes/{id}/apply", appear.applyTheme)
	api.HandleFunc("PUT /api/v1/appearance/placement", appear.setPlacement)
	api.HandleFunc("GET /api/v1/appearance/folders", appear.listFolders)
	api.HandleFunc("POST /api/v1/appearance/folders", appear.createFolder)
	api.HandleFunc("PATCH /api/v1/appearance/folders/{id}", appear.updateFolder)
	api.HandleFunc("DELETE /api/v1/appearance/folders/{id}", appear.deleteFolder)
	api.HandleFunc("POST /api/v1/appearance/images/{id}/move", appear.moveImage)
	api.HandleFunc("GET /api/v1/appearance/gallery", appear.gallery)

	fin := &financeHandlers{store: st, rates: ratesCache}
	api.HandleFunc("GET /api/v1/finance/overview", fin.overview)
	api.HandleFunc("GET /api/v1/finance/plans", fin.listPlans)
	api.HandleFunc("POST /api/v1/finance/plans", fin.createPlan)
	api.HandleFunc("PUT /api/v1/finance/plans/{id}", fin.updatePlan)
	api.HandleFunc("POST /api/v1/finance/plans/{id}/archive", fin.archivePlan)
	api.HandleFunc("DELETE /api/v1/finance/plans/{id}", fin.deletePlan)
	api.HandleFunc("POST /api/v1/finance/plans/{id}/pay", fin.pay)
	api.HandleFunc("POST /api/v1/finance/plans/{id}/skip", fin.skip)
	api.HandleFunc("GET /api/v1/finance/payments", fin.listPayments)
	api.HandleFunc("GET /api/v1/finance/debts", fin.listDebts)
	api.HandleFunc("POST /api/v1/finance/debts", fin.createDebt)
	api.HandleFunc("PUT /api/v1/finance/debts/{id}", fin.updateDebt)
	api.HandleFunc("POST /api/v1/finance/debts/{id}/payments", fin.addDebtPayment)
	api.HandleFunc("GET /api/v1/finance/debts/{id}/payments", fin.listDebtPayments)
	api.HandleFunc("POST /api/v1/finance/debts/{id}/status", fin.setDebtStatus)
	api.HandleFunc("POST /api/v1/finance/debts/{id}/archive", fin.archiveDebt)
	api.HandleFunc("DELETE /api/v1/finance/debts/{id}", fin.deleteDebt)
	api.HandleFunc("GET /api/v1/finance/settings", fin.getSettings)
	api.HandleFunc("PUT /api/v1/finance/settings", fin.setSettings)
	// фазы 3-4: фактические траты, дерево категорий, счета, цели, отчёт
	api.HandleFunc("GET /api/v1/finance/refs", fin.refs)
	api.HandleFunc("GET /api/v1/finance/stats", fin.stats)
	api.HandleFunc("GET /api/v1/tv/rooms", tvh.rooms)
	api.HandleFunc("POST /api/v1/tv/attach", tvh.attach)
	api.HandleFunc("DELETE /api/v1/tv/rooms/{key}", tvh.deleteRoom)
	api.HandleFunc("GET /api/v1/finance/categories", fin.listCategories)
	api.HandleFunc("POST /api/v1/finance/categories", fin.createCategory)
	api.HandleFunc("POST /api/v1/finance/categories/seed", fin.seedCategories)
	api.HandleFunc("PUT /api/v1/finance/categories/{id}", fin.updateCategory)
	api.HandleFunc("POST /api/v1/finance/categories/{id}/archive", fin.archiveCategory)
	api.HandleFunc("DELETE /api/v1/finance/categories/{id}", fin.deleteCategory)
	api.HandleFunc("GET /api/v1/finance/transactions", fin.listTx)
	api.HandleFunc("POST /api/v1/finance/transactions", fin.createTx)
	api.HandleFunc("PUT /api/v1/finance/transactions/{id}", fin.updateTx)
	api.HandleFunc("DELETE /api/v1/finance/transactions/{id}", fin.deleteTx)
	api.HandleFunc("GET /api/v1/finance/accounts", fin.listAccounts)
	api.HandleFunc("POST /api/v1/finance/accounts", fin.createAccount)
	api.HandleFunc("PUT /api/v1/finance/accounts/{id}", fin.updateAccount)
	api.HandleFunc("POST /api/v1/finance/accounts/{id}/archive", fin.archiveAccount)
	api.HandleFunc("DELETE /api/v1/finance/accounts/{id}", fin.deleteAccount)
	api.HandleFunc("GET /api/v1/finance/goals", fin.listGoals)
	api.HandleFunc("POST /api/v1/finance/goals", fin.createGoal)
	api.HandleFunc("PUT /api/v1/finance/goals/{id}", fin.updateGoal)
	api.HandleFunc("POST /api/v1/finance/goals/{id}/archive", fin.archiveGoal)
	api.HandleFunc("DELETE /api/v1/finance/goals/{id}", fin.deleteGoal)
	api.HandleFunc("GET /api/v1/finance/goals/{id}/moves", fin.listGoalMoves)
	api.HandleFunc("POST /api/v1/finance/goals/{id}/moves", fin.addGoalMove)
	api.HandleFunc("DELETE /api/v1/finance/goals/{id}/moves/{move_id}", fin.deleteGoalMove)

	mail := &mailHandlers{store: st, domains: mailInfo.Domains,
		hostname: mailInfo.Hostname, unblock: mailInfo.Unblock,
		receipts: mailInfo.Receipts, authMW: authMW}
	if len(mail.domains) == 0 {
		mail.domains = []string{"example.com"} // чтобы форма адреса не падала в dev
	}
	api.HandleFunc("GET /api/v1/mail/overview", mail.overview)
	api.HandleFunc("GET /api/v1/mail/guard", mail.guard)
	api.HandleFunc("POST /api/v1/mail/guard/{ip}/unblock", mail.unblockIP)
	api.HandleFunc("PUT /api/v1/mail/settings", mail.setSettings)
	api.HandleFunc("GET /api/v1/mail/messages/{id}", mail.message)
	api.HandleFunc("POST /api/v1/mail/messages/{id}/flag", mail.setFlag)
	api.HandleFunc("POST /api/v1/mail/messages/{id}/archive", mail.archive)
	api.HandleFunc("DELETE /api/v1/mail/messages/{id}", mail.deleteMessage)
	api.HandleFunc("GET /api/v1/mail/attachments/{id}", mail.attachment)
	api.HandleFunc("POST /api/v1/mail/addresses", mail.createAddress)
	api.HandleFunc("PUT /api/v1/mail/addresses/{id}", mail.updateAddress)
	api.HandleFunc("DELETE /api/v1/mail/addresses/{id}", mail.deleteAddress)
	api.HandleFunc("PUT /api/v1/mail/addresses/{id}/parser", mail.setAddressParser)

	// группы товаров, разметка позиций чека, доли траты и история цен
	items := &itemsHandlers{store: st, receipts: mailInfo.Receipts, hub: aiHub}
	api.HandleFunc("GET /api/v1/finance/item-groups", items.groups)
	api.HandleFunc("POST /api/v1/finance/item-groups/seed", items.seedGroups)
	api.HandleFunc("PUT /api/v1/finance/item-groups/{code}", items.setGroup)
	api.HandleFunc("GET /api/v1/finance/items/unclassified", items.unclassified)
	api.HandleFunc("POST /api/v1/finance/items/assign", items.assign)
	api.HandleFunc("GET /api/v1/finance/items/rules", items.rules)
	api.HandleFunc("POST /api/v1/finance/items/words", items.createWord)
	api.HandleFunc("DELETE /api/v1/finance/items/words/{id}", items.deleteWord)
	api.HandleFunc("DELETE /api/v1/finance/items/rules/{id}", items.deleteRule)
	api.HandleFunc("GET /api/v1/finance/items/top", items.topItems)
	api.HandleFunc("GET /api/v1/finance/items/prices", items.itemPrices)
	api.HandleFunc("POST /api/v1/finance/items/suggest", items.suggest)
	api.HandleFunc("GET /api/v1/finance/items/suggest/{run_id}", items.suggestions)
	api.HandleFunc("POST /api/v1/finance/receipts/{id}/classify", items.classify)
	api.HandleFunc("GET /api/v1/finance/transactions/{id}/splits", items.txSplits)
	api.HandleFunc("PUT /api/v1/finance/transactions/{id}/splits", items.setTxSplits)
	api.HandleFunc("POST /api/v1/mail/messages/{id}/parse", mail.parseMessage)
	api.HandleFunc("GET /api/v1/mail/receipts/{id}/items", mail.receiptItems)
	api.HandleFunc("DELETE /api/v1/mail/receipts/{id}", mail.deleteReceipt)

	diary := &diaryHandlers{store: st}
	api.HandleFunc("GET /api/v1/diary/entries", diary.list)
	api.HandleFunc("POST /api/v1/diary/entries", diary.create)
	api.HandleFunc("PATCH /api/v1/diary/entries/{id}", diary.update)
	api.HandleFunc("DELETE /api/v1/diary/entries/{id}", diary.delete)

	links := &linksHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/links/tree", links.tree)
	api.HandleFunc("PUT /api/v1/links/tree", links.replaceAll)
	api.HandleFunc("POST /api/v1/links/folders", links.createFolder)
	api.HandleFunc("PATCH /api/v1/links/folders/{id}", links.updateFolder)
	api.HandleFunc("DELETE /api/v1/links/folders/{id}", links.deleteFolder)
	api.HandleFunc("POST /api/v1/links/folders/{id}/share", links.sendFolder)
	api.HandleFunc("POST /api/v1/links/folders/{id}/share-token", links.shareFolderToken)
	api.HandleFunc("POST /api/v1/links/folders/redeem", links.redeemFolder)
	api.HandleFunc("POST /api/v1/links/{id}/share", links.sendLink)
	api.HandleFunc("POST /api/v1/links/{id}/share-token", links.shareLinkToken)
	api.HandleFunc("POST /api/v1/links/redeem", links.redeemLink)
	api.HandleFunc("POST /api/v1/links", links.createLink)
	api.HandleFunc("PATCH /api/v1/links/{id}", links.updateLink)
	api.HandleFunc("DELETE /api/v1/links/{id}", links.deleteLink)
	api.HandleFunc("POST /api/v1/links/{id}/click", links.click)
	api.HandleFunc("GET /api/v1/links/storage", links.getStorage)
	api.HandleFunc("PUT /api/v1/links/storage", links.setStorage)

	articles := &articlesHandlers{store: st, bot: bot, dataDir: dataDir}
	api.HandleFunc("GET /api/v1/articles/tree", articles.tree)
	api.HandleFunc("GET /api/v1/articles/{id}", articles.get)
	api.HandleFunc("POST /api/v1/articles", articles.create)
	api.HandleFunc("PATCH /api/v1/articles/{id}", articles.update)
	api.HandleFunc("DELETE /api/v1/articles/{id}", articles.delete)
	api.HandleFunc("POST /api/v1/articles/folders", articles.createFolder)
	api.HandleFunc("PATCH /api/v1/articles/folders/{id}", articles.updateFolder)
	api.HandleFunc("DELETE /api/v1/articles/folders/{id}", articles.deleteFolder)
	api.HandleFunc("POST /api/v1/articles/{id}/share-token", articles.shareToken)
	api.HandleFunc("POST /api/v1/articles/{id}/download-token", articles.downloadToken)
	api.HandleFunc("POST /api/v1/articles/{id}/read-token", articles.readToken)
	api.HandleFunc("GET /api/v1/articles/{id}/history", articles.history)
	// отдельный префикс: /articles/revisions/{id} конфликтует с /articles/{id}/history
	api.HandleFunc("GET /api/v1/article-revisions/{id}", articles.revision)
	api.HandleFunc("PUT /api/v1/articles/{id}/read-pos", articles.setReadPos)
	api.HandleFunc("POST /api/v1/articles/images", articles.uploadImage)
	api.HandleFunc("POST /api/v1/articles/{id}/send", articles.send)
	api.HandleFunc("POST /api/v1/articles/redeem", articles.redeem)
	api.HandleFunc("GET /api/v1/articles/search", articles.searchContent)
	api.HandleFunc("POST /api/v1/articles/folders/{id}/share", articles.shareFolder)
	api.HandleFunc("GET /api/v1/articles/folders/{id}/shares", articles.listFolderShares)
	api.HandleFunc("DELETE /api/v1/articles/folders/{id}/shares/{userID}", articles.revokeFolderShare)
	api.HandleFunc("DELETE /api/v1/articles/shared/{id}", articles.leaveShared)

	passwords := &passwordsHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/passwords/vault", passwords.getVault)
	api.HandleFunc("PUT /api/v1/passwords/vault", passwords.putVault)
	api.HandleFunc("DELETE /api/v1/passwords/vault", passwords.deleteVault)
	api.HandleFunc("GET /api/v1/passwords/shares", passwords.listShares)
	api.HandleFunc("POST /api/v1/passwords/shares", passwords.createShare)
	api.HandleFunc("DELETE /api/v1/passwords/shares/{id}", passwords.deleteShare)

	// токены доступа (веб/расширение) — выпуск только из Telegram,
	// путь закрыт для токен-сессий в auth.Middleware
	tokens := &tokensHandlers{store: st}
	api.HandleFunc("GET /api/v1/settings/tokens", tokens.list)
	api.HandleFunc("POST /api/v1/settings/tokens", tokens.create)
	api.HandleFunc("DELETE /api/v1/settings/tokens/{id}", tokens.revoke)

	settings := &settingsHandlers{store: st, dataDir: dataDir}
	// --- Сейф: шифрование на клиенте, сервер хранит только шифротекст ---
	vault := &vaultHandlers{store: st, bot: bot, dataDir: dataDir, uploads: map[string]*vaultUpload{}}
	// уборщик сейфа: просроченные файлы, ссылки и брошенные загрузки
	go vault.sweep(context.Background())
	api.HandleFunc("GET /api/v1/vault", vault.tree)
	api.HandleFunc("POST /api/v1/vault/folders", vault.createFolder)
	api.HandleFunc("PATCH /api/v1/vault/folders/{id}", vault.updateFolder)
	api.HandleFunc("DELETE /api/v1/vault/folders/{id}", vault.deleteFolder)
	api.HandleFunc("POST /api/v1/vault/uploads", vault.initUpload)
	api.HandleFunc("POST /api/v1/vault/uploads/{uid}/chunk", vault.uploadChunk)
	api.HandleFunc("POST /api/v1/vault/uploads/{uid}/finish", vault.finishUpload)
	api.HandleFunc("GET /api/v1/vault/files/{id}/blob", vault.blob(false))
	api.HandleFunc("GET /api/v1/vault/files/{id}/thumb", vault.blob(true))
	api.HandleFunc("PATCH /api/v1/vault/files/{id}", vault.updateFile)
	api.HandleFunc("POST /api/v1/vault/files/delete", vault.deleteFiles)
	api.HandleFunc("POST /api/v1/vault/files/expiry", vault.setExpiry)
	api.HandleFunc("GET /api/v1/vault/uploads/{uid}", vault.uploadStatus)
	api.HandleFunc("POST /api/v1/vault/files/{id}/copy", vault.copyFile)
	api.HandleFunc("GET /api/v1/vault/files/{id}/access", vault.accessLog)
	api.HandleFunc("POST /api/v1/vault/files/{id}/links", vault.createLink)
	api.HandleFunc("GET /api/v1/vault/files/{id}/links", vault.listLinks)
	api.HandleFunc("DELETE /api/v1/vault/links/{id}", vault.revokeLink)
	api.HandleFunc("POST /api/v1/vault/folders/{id}/share", vault.share("folder"))
	api.HandleFunc("GET /api/v1/vault/folders/{id}/shares", vault.listShares("folder"))
	api.HandleFunc("DELETE /api/v1/vault/folders/{id}/share/{userID}", vault.revokeShare("folder"))
	api.HandleFunc("POST /api/v1/vault/files/{id}/share", vault.share("file"))
	api.HandleFunc("GET /api/v1/vault/files/{id}/shares", vault.listShares("file"))
	api.HandleFunc("DELETE /api/v1/vault/files/{id}/share/{userID}", vault.revokeShare("file"))

	api.HandleFunc("GET /api/v1/settings/collapsed", settings.getCollapsed)
	api.HandleFunc("PUT /api/v1/settings/collapsed", settings.setCollapsed)
	api.HandleFunc("GET /api/v1/settings/appearance", settings.getAppearance)
	api.HandleFunc("PUT /api/v1/settings/appearance", settings.setAppearance)
	api.HandleFunc("GET /api/v1/settings/headers", settings.getHeaders)
	api.HandleFunc("PUT /api/v1/settings/headers", settings.setHeaders)
	api.HandleFunc("GET /api/v1/settings/background", settings.getBackground)
	api.HandleFunc("PUT /api/v1/settings/background", settings.setBackground)
	api.HandleFunc("POST /api/v1/settings/background/upload", settings.uploadBackground)
	api.HandleFunc("DELETE /api/v1/settings/background/images/{id}", settings.deleteBackgroundImage)

	metrics := &metricsHandlers{store: st}
	api.HandleFunc("GET /api/v1/metrics/chart-types", metrics.chartTypes)
	api.HandleFunc("GET /api/v1/metrics/tree", metrics.tree)
	api.HandleFunc("POST /api/v1/metrics/categories", metrics.createCategory)
	api.HandleFunc("PATCH /api/v1/metrics/categories/{id}", metrics.renameCategory)
	api.HandleFunc("DELETE /api/v1/metrics/categories/{id}", metrics.deleteCategory)
	api.HandleFunc("POST /api/v1/metrics/categories/{id}/items", metrics.createItem)
	api.HandleFunc("PATCH /api/v1/metrics/items/{id}", metrics.updateItem)
	api.HandleFunc("DELETE /api/v1/metrics/items/{id}", metrics.deleteItem)
	api.HandleFunc("GET /api/v1/metrics/items/{id}/values", metrics.listValues)
	api.HandleFunc("POST /api/v1/metrics/items/{id}/values", metrics.addValues)
	api.HandleFunc("PATCH /api/v1/metrics/values/{id}", metrics.updateValue)
	api.HandleFunc("DELETE /api/v1/metrics/values/{id}", metrics.deleteValue)

	converter := &converterHandlers{store: st, ratesCache: ratesCache}
	api.HandleFunc("GET /api/v1/converter/currencies", converter.listCurrencies)
	api.HandleFunc("POST /api/v1/converter/currencies", converter.addCurrency)
	api.HandleFunc("DELETE /api/v1/converter/currencies/{code}", converter.removeCurrency)
	api.HandleFunc("GET /api/v1/converter/available", converter.available)
	api.HandleFunc("GET /api/v1/converter/history", converter.history)
	api.HandleFunc("GET /api/v1/converter/rates", converter.rates)

	reminders := &remindersHandlers{store: st, bot: bot}
	api.HandleFunc("GET /api/v1/reminders", reminders.list)
	api.HandleFunc("GET /api/v1/reminders/upcoming", reminders.upcoming)
	api.HandleFunc("POST /api/v1/reminders", reminders.create)
	api.HandleFunc("PUT /api/v1/reminders/{id}", reminders.update)
	api.HandleFunc("PATCH /api/v1/reminders/{id}/enabled", reminders.toggle)
	api.HandleFunc("DELETE /api/v1/reminders/{id}", reminders.delete)
	// отдельный префикс: /reminders/categories/{id} конфликтует с /reminders/{id}/enabled
	api.HandleFunc("GET /api/v1/reminder-categories", reminders.listGroups)
	api.HandleFunc("POST /api/v1/reminder-categories", reminders.createGroup)
	api.HandleFunc("PATCH /api/v1/reminder-categories/{id}", reminders.renameGroup)
	api.HandleFunc("DELETE /api/v1/reminder-categories/{id}", reminders.deleteGroup)
	api.HandleFunc("POST /api/v1/reminder-categories/{id}/share-token", reminders.shareGroupToken)
	api.HandleFunc("POST /api/v1/reminder-categories/{id}/send", reminders.sendGroup)
	api.HandleFunc("POST /api/v1/reminder-categories/redeem", reminders.redeemGroup)
	api.HandleFunc("POST /api/v1/reminder-categories/import", reminders.importGroup)

	rel := &releasesHandlers{store: st, authMW: authMW}
	api.HandleFunc("GET /api/v1/releases", rel.list)

	admin := &adminHandlers{store: st, authMW: authMW}
	api.HandleFunc("PATCH /api/v1/releases/{id}", admin.adminOnly(rel.update))
	api.HandleFunc("POST /api/v1/admin/tests/import", admin.adminOnly(tests.importDeck))
	api.HandleFunc("POST /api/v1/admin/gallery/categories", admin.adminOnly(appear.createGalleryCategory))
	api.HandleFunc("PATCH /api/v1/admin/gallery/categories/{id}", admin.adminOnly(appear.updateGalleryCategory))
	api.HandleFunc("DELETE /api/v1/admin/gallery/categories/{id}", admin.adminOnly(appear.deleteGalleryCategory))
	api.HandleFunc("POST /api/v1/admin/gallery/images", admin.adminOnly(appear.uploadGalleryImage))
	api.HandleFunc("POST /api/v1/admin/gallery/images/from-background", admin.adminOnly(appear.galleryFromBackground))
	api.HandleFunc("PATCH /api/v1/admin/gallery/images/{id}", admin.adminOnly(appear.updateGalleryImage))
	api.HandleFunc("DELETE /api/v1/admin/gallery/images/{id}", admin.adminOnly(appear.deleteGalleryImage))
	api.HandleFunc("GET /api/v1/admin/users", admin.adminOnly(admin.listUsers))
	api.HandleFunc("GET /api/v1/admin/users/{id}", admin.adminOnly(admin.getUser))
	api.HandleFunc("POST /api/v1/admin/users/{id}/ban", admin.adminOnly(admin.setBanned))
	api.HandleFunc("POST /api/v1/admin/users/{id}/type", admin.adminOnly(admin.setUserType))
	api.HandleFunc("GET /api/v1/admin/limits", admin.adminOnly(admin.listLimits))
	api.HandleFunc("PUT /api/v1/admin/limits/{type}", admin.adminOnly(admin.updateLimits))
	api.HandleFunc("GET /api/v1/admin/users/search", admin.adminOnly(access.searchUsers))
	api.HandleFunc("GET /api/v1/admin/pages", admin.adminOnly(access.adminPages))
	api.HandleFunc("PUT /api/v1/admin/pages/{page}", admin.adminOnly(access.setVisibility))
	api.HandleFunc("POST /api/v1/admin/pages/{page}/access", admin.adminOnly(access.addGrant("page_access", "page")))
	api.HandleFunc("DELETE /api/v1/admin/pages/{page}/access/{userID}", admin.adminOnly(access.removeGrant("page_access", "page")))
	api.HandleFunc("POST /api/v1/admin/features/{feature}/access", admin.adminOnly(access.addGrant("feature_access", "feature")))
	api.HandleFunc("DELETE /api/v1/admin/features/{feature}/access/{userID}", admin.adminOnly(access.removeGrant("feature_access", "feature")))

	mux := http.NewServeMux()
	// архивы расширения браузера — публично, без авторизации (это просто
	// клиент; доступ к данным всё равно только по токену)
	mux.HandleFunc("GET /ext/{name}", extensionHandler(exts))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("/api/v1/", authMW.Wrap(access.pageGuard(api)))
	// отчёты push-агентов (домашние машины) — вне tma-авторизации,
	// агент представляется Bearer push-токеном; литеральный паттерн
	// побеждает поддерево /api/v1/
	mux.HandleFunc("POST /api/v1/agent/push", srvH.agentPush)
	// файловый агент: WS-соединение (авторизация токеном машины) и отдача
	// файла по одноразовому пропуску — оба вне tma-авторизации
	mux.HandleFunc("GET /api/v1/files/agent", fh.agentWS)
	mux.HandleFunc("GET /api/v1/files/stream/{ticket}", fh.stream)
	mux.HandleFunc("HEAD /api/v1/files/stream/{ticket}", fh.stream)
	// консоль: WS агента (Bearer токен машины) и WS браузера (одноразовый
	// пропуск) — оба вне tma-авторизации
	mux.HandleFunc("GET /api/v1/automation/agent", autom.agentWS)
	mux.HandleFunc("GET /api/v1/ai/agent", ai.agentWS)
	mux.HandleFunc("GET /api/v1/terminal/agent", th.agentWS)
	// ТВ-плеер: веб-сокет приставки (она не авторизована — её открывают на
	// телевизоре) и веб-сокет пульта по одноразовому пропуску
	mux.HandleFunc("GET /api/v1/tv/socket", tvh.tvWS)
	mux.HandleFunc("GET /api/v1/tv/remote/{ticket}", tvh.remoteWS)
	mux.HandleFunc("GET /api/v1/terminal/stream/{ticket}", th.stream)
	// временная ссылка на файл сейфа — вне авторизации; расшифровать всё
	// равно нечем: пароль знает только тот, кому его передали
	mux.HandleFunc("GET /api/v1/vault/public/links/{token}", vault.publicLink)
	mux.HandleFunc("GET /api/v1/vault/public/links/{token}/blob", vault.publicLinkBlob)
	// публичная ссылка на скачивание статьи (.md) — вне авторизации
	mux.HandleFunc("GET /dl/articles/{token}", articles.publicDownload)
	// публичное чтение статьи: JSON для SPA-страницы /read/{token}
	mux.HandleFunc("GET /api/v1/articles/public/{token}", articles.publicRead)
	// Загруженные фоны раздаются публично: <img>/CSS не умеют слать
	// Authorization, защита — невосстановимые случайные имена файлов.
	mux.Handle("GET /uploads/backgrounds/", cacheStatic(
		http.StripPrefix("/uploads/backgrounds/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "backgrounds"))))))
	// картинки статей — тот же принцип (случайные имена)
	mux.Handle("GET /uploads/articles/", cacheStatic(
		http.StripPrefix("/uploads/articles/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "articles"))))))
	// фото контактов — тот же принцип (случайные имена)
	mux.Handle("GET /uploads/contacts/", cacheStatic(
		http.StripPrefix("/uploads/contacts/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "contacts"))))))
	mux.Handle("GET /uploads/food/", cacheStatic(
		http.StripPrefix("/uploads/food/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "food"))))))
	mux.Handle("GET /uploads/projects/", cacheStatic(
		http.StripPrefix("/uploads/projects/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "projects"))))))
	// общая галерея фонов — общий контент, наполняется админом
	mux.Handle("GET /uploads/gallery/", cacheStatic(
		http.StripPrefix("/uploads/gallery/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "gallery"))))))
	// картинки вопросов тестов — общий контент, не пользовательские загрузки
	mux.Handle("GET /uploads/tests/", cacheStatic(
		http.StripPrefix("/uploads/tests/",
			http.FileServer(http.Dir(filepath.Join(dataDir, "tests"))))))
	if staticDir != "" {
		mux.Handle("/", spaHandler(staticDir))
	}

	handler := recoverer(requestLogger(mux, logger), logger)
	if devCORS {
		handler = corsAllowAll(handler)
	}
	return handler
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

// spaHandler раздаёт собранный фронтенд: существующие файлы как есть
// (хэшированные ассеты — с иммутабельным кэшем), всё остальное — index.html.
func spaHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFile(w, r, path)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

func requestLogger(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func recoverer(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic", "recover", rec, "path", r.URL.Path)
				internalError(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// corsAllowAll is only enabled together with DEV_AUTH_BYPASS for local
// development; in production the frontend is served same-origin.
func corsAllowAll(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
