package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aichain/ai-chain/internal/crypto"
)

// ---- Millisecond-precision tracing ----
var traceMu sync.Mutex

func trace(format string, args ...interface{}) {
	traceMu.Lock()
	defer traceMu.Unlock()
	now := time.Now()
	ts := now.Format("15:04:05.000") // millisecond precision
	msg := fmt.Sprintf(format, args...)
	log.Printf("[%s] %s", ts, msg)
}

//go:embed dashboard.html
var dashboardHTML string

// ---- Task Pipeline ----
// Pending → Analyzing (split into subtasks) → Subtask Mining → Aggregation → Completed

type SubtaskView struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Index     int    `json:"index"`
	Prompt    string `json:"prompt"`
	Status    string `json:"status"` // Pending, Running, Completed, Failed
	Result    string `json:"result,omitempty"`
}

type TaskView struct {
	ID         string         `json:"id"`
	Creator    string         `json:"creator"`
	Tier       string         `json:"tier"`
	Model      string         `json:"model"`
	Prompt     string         `json:"prompt"`
	Fee        string         `json:"fee"`
	Status     string         `json:"status"` // Pending, Analyzing, Mining, Aggregating, Completed, Failed
	Subtasks   []SubtaskView  `json:"subtasks,omitempty"`
	Result     string         `json:"result,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

type BlockView struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
	Txs    int    `json:"txs"`
	Time   string `json:"time"`
}

type TxView struct {
	Hash   string `json:"hash"`
	Type   string `json:"type"`
	Amount string `json:"amount"`
	Time   string `json:"time"`
}

type RealNode struct {
	mu           sync.RWMutex
	startedAt    time.Time
	peerID       string
	minerAddr    string
	minerName    string
	gpuName      string
	gpuCount     int
	vramMB       uint64
	ramMB        uint64
	models       []string
	blocks       []BlockView
	tasks        []TaskView
	subtasks     []SubtaskView
	transactions []TxView
	peerCount    int
	mempoolSize  int
	wsClients    []chan string
	liveStream   []chan string // SSE clients watching current inference
	currentMining string       // subtask ID currently being mined
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Auto-resolve port conflict
	for i := 0; i < 5; i++ {
		ln, err := net.Listen("tcp", ":"+port)
		if err == nil {
			ln.Close()
			break
		}
		log.Printf("Port %s in use, killing old process (attempt %d/5)...", port, i+1)
		exec.Command("fuser", "-k", port+"/tcp").Run()
		time.Sleep(1 * time.Second)
	}

	pub, _, _ := crypto.GenerateKey()
	addr := crypto.PubKeyToAddress(pub)
	peerID := fmt.Sprintf("%x", addr[:8])

	gpuName, gpuCount, vramMB, ramMB, models := detectHardware()

	node := &RealNode{
		startedAt:    time.Now(),
		peerID:       peerID,
		minerAddr:    addr.Hex(),
		minerName:    fmt.Sprintf("Miner-%s", peerID),
		gpuName:      gpuName,
		gpuCount:     gpuCount,
		vramMB:       vramMB,
		ramMB:        ramMB,
		models:       models,
		blocks:       make([]BlockView, 0),
		tasks:        make([]TaskView, 0),
		subtasks:     make([]SubtaskView, 0),
		transactions: make([]TxView, 0),
		peerCount:    1,
		wsClients:    make([]chan string, 0),
	}

	log.Printf("Miner: %s (%s) | HW: %s %dGPU %dMB_VRAM %dMB_RAM | Models: %v",
		node.minerName, node.peerID, node.gpuName, node.gpuCount, node.vramMB, node.ramMB, node.models)

	node.blocks = append(node.blocks, BlockView{Height: 0, Hash: fmt.Sprintf("%064x", 0), Txs: 0, Time: time.Now().Format("15:04:05")})

	go node.autoMiner()
	go node.taskAnalyzer()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
	})
	mux.HandleFunc("/api/node", node.handleNode)
	mux.HandleFunc("/api/blocks", node.handleBlocks)
	mux.HandleFunc("/api/tasks", node.handleTasks)
	mux.HandleFunc("/api/tasks/mine", node.handleUserTasks)
	mux.HandleFunc("/api/tasks/publish", node.handlePublishTask)
	mux.HandleFunc("/api/mine", node.handleMine)
	mux.HandleFunc("/api/transactions", node.handleTransactions)
	mux.HandleFunc("/api/stream/current", node.handleLiveStream)
	mux.HandleFunc("/ws", node.handleWebSocket)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; os.Exit(0) }()

	log.Printf("🌐 http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// ---- API Handlers ----

func (n *RealNode) handleNode(w http.ResponseWriter, r *http.Request) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	writeJSON(w, map[string]interface{}{
		"miner_name": n.minerName, "miner_addr": n.minerAddr, "peer_id": n.peerID,
		"gpu": n.gpuName, "gpu_count": n.gpuCount, "vram_mb": n.vramMB, "ram_mb": n.ramMB,
		"models": n.models, "uptime": fmt.Sprintf("%dm", int(time.Since(n.startedAt).Minutes())),
		"peers": n.peerCount, "height": len(n.blocks)-1, "mempool": n.mempoolSize,
		"task_pool": len(n.tasks), "subtask_pool": len(n.subtasks),
	})
}

func (n *RealNode) handleBlocks(w http.ResponseWriter, r *http.Request) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	writeJSON(w, n.blocks)
}

func (n *RealNode) handleTasks(w http.ResponseWriter, r *http.Request) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	writeJSON(w, n.tasks)
}

func (n *RealNode) handleUserTasks(w http.ResponseWriter, r *http.Request) {
	creator := r.URL.Query().Get("creator")
	n.mu.RLock()
	defer n.mu.RUnlock()
	if creator == "" {
		writeJSON(w, n.tasks)
		return
	}
	var mine []TaskView
	for _, t := range n.tasks {
		if t.Creator == creator {
			mine = append(mine, t)
		}
	}
	writeJSON(w, mine)
}

func (n *RealNode) handleTransactions(w http.ResponseWriter, r *http.Request) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	writeJSON(w, n.transactions)
}

func (n *RealNode) handlePublishTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, map[string]string{"error": "POST required"})
		return
	}
	var input struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		Tier    string `json:"tier"`
		Fee     string `json:"fee"`
		Creator string `json:"creator"`
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.Prompt == "" {
		writeJSON(w, map[string]string{"error": "prompt is required"})
		return
	}
	if input.Model == "" && len(n.models) > 0 {
		input.Model = n.models[0]
	}
	if input.Creator == "" {
		input.Creator = "anonymous"
	}

	task := TaskView{
		ID:        fmt.Sprintf("task-%x", sha256.Sum256([]byte(input.Prompt+time.Now().String())))[:24],
		Creator:   input.Creator,
		Tier:      input.Tier,
		Model:     input.Model,
		Prompt:    input.Prompt,
		Fee:       input.Fee + " APT",
		Status:    "Pending",
		Subtasks:  nil,
		CreatedAt: time.Now().Format("15:04:05"),
	}
	trace("API: publishTask id=%s creator=%s prompt='%s' model=%s", task.ID, task.Creator, task.Prompt[:min(50,len(task.Prompt))], task.Model)

	trace("API: acquiring lock to add task to pool")
	n.mu.Lock()
	n.tasks = append([]TaskView{task}, n.tasks...)
	n.mempoolSize++
	trace("API: task added, pool now %d tasks, %d mempool", len(n.tasks), n.mempoolSize)
	n.mu.Unlock()
	trace("API: lock released, broadcasting")

	n.broadcast("tasks")
	writeJSON(w, map[string]interface{}{"status": "published", "task": task})
}

func (n *RealNode) handleMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, map[string]string{"error": "POST required"})
		return
	}
	var input struct {
		SubtaskID string `json:"subtask_id"`
		MinerID   string `json:"miner_id"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	n.mu.Lock()
	var target *SubtaskView
	var targetIdx int
	for i := range n.subtasks {
		if n.subtasks[i].ID == input.SubtaskID && n.subtasks[i].Status == "Pending" {
			target = &n.subtasks[i]
			targetIdx = i
			break
		}
	}
	if target == nil {
		n.mu.Unlock()
		writeJSON(w, map[string]string{"error": "subtask not found or already claimed"})
		return
	}
	n.subtasks[targetIdx].Status = "Running"
	n.mu.Unlock()
	n.broadcast("tasks")

	token := os.Getenv("LMSTUDIO_TOKEN")
	if token == "" {
		log.Printf("WARNING: LMSTUDIO_TOKEN env not set. AI mining disabled. Set: export LMSTUDIO_TOKEN=your_token")
	}

	result, err := runRealInference("qwen3.6-35b-a3b-mtp", target.Prompt, token)
	n.mu.Lock()
	if err != nil {
		n.subtasks[targetIdx].Status = "Failed"
		n.subtasks[targetIdx].Result = fmt.Sprintf("Error: %v", err)
	} else {
		n.subtasks[targetIdx].Status = "Completed"
		n.subtasks[targetIdx].Result = result
	}

	// Check if all subtasks for the parent task are complete
	parentID := target.TaskID
	allDone := true
	var results []string
	for _, st := range n.subtasks {
		if st.TaskID == parentID {
			if st.Status != "Completed" && st.Status != "Failed" {
				allDone = false
			}
			if st.Result != "" {
				results = append(results, fmt.Sprintf("[Subtask %d]: %s", st.Index, st.Result))
			}
		}
	}

	// If all subtasks done, mark parent task as Completed and aggregate results
	if allDone {
		for i := range n.tasks {
			if n.tasks[i].ID == parentID {
				n.tasks[i].Status = "Completed"
				n.tasks[i].Result = strings.Join(results, "\n\n---\n\n")
				n.tasks[i].Subtasks = n.filterSubtasks(parentID)

				newBlock := BlockView{
					Height: uint64(len(n.blocks)),
					Hash:   fmt.Sprintf("%064x", time.Now().UnixNano()),
					Txs:    1,
					Time:   time.Now().Format("15:04:05"),
				}
				n.blocks = append(n.blocks, newBlock)
				n.transactions = append(n.transactions, TxView{
					Hash: parentID, Type: "TaskCompleted", Amount: n.tasks[i].Fee, Time: time.Now().Format("15:04:05"),
				})
				if n.mempoolSize > 0 {
					n.mempoolSize--
				}
				log.Printf("[Pipeline] Task %s COMPLETED — %d subtasks aggregated", parentID[:12], len(results))
				break
			}
		}
	}
	n.mu.Unlock()

	n.broadcast("tasks")
	n.broadcast("blocks")
	writeJSON(w, map[string]interface{}{
		"status": "completed", "subtask_id": target.ID, "result": result,
	})
}

func (n *RealNode) filterSubtasks(taskID string) []SubtaskView {
	var result []SubtaskView
	for _, st := range n.subtasks {
		if st.TaskID == taskID {
			result = append(result, st)
		}
	}
	return result
}

// ---- Pipeline Workers ----

// taskAnalyzer splits new tasks into subtasks.
func (n *RealNode) taskAnalyzer() {
	trace("ANALYZER: goroutine started")
	for {
		time.Sleep(2 * time.Second)
		trace("ANALYZER: scanning for Pending tasks (total tasks=%d)", len(n.tasks))
		n.mu.Lock()
		trace("ANALYZER: lock acquired, iterating %d tasks", len(n.tasks))
		found := 0
		for i := range n.tasks {
			if n.tasks[i].Status != "Pending" {
				continue
			}
			found++
			task := &n.tasks[i]
			trace("ANALYZER: found Pending task[%d] id=%s prompt='%s'", i, task.ID, task.Prompt[:min(50,len(task.Prompt))])
			trace("ANALYZER: task[%d] status Pending → Analyzing", i)
			task.Status = "Analyzing"

			subtasks := splitTask(task)
			task.Subtasks = subtasks
			trace("ANALYZER: splitTask returned %d subtask(s)", len(subtasks))
			for si, st := range subtasks {
				trace("ANALYZER:   subtask[%d] id=%s index=%d prompt='%s' status=%s",
					si, st.ID, st.Index, st.Prompt[:min(40,len(st.Prompt))], st.Status)
				n.subtasks = append(n.subtasks, st)
			}
			trace("ANALYZER: task[%d] status Analyzing → Mining (total subtasks now=%d)", i, len(n.subtasks))
			task.Status = "Mining"
		}
		if found == 0 {
			trace("ANALYZER: no Pending tasks found")
		}
		trace("ANALYZER: releasing lock after processing %d tasks", found)
		n.mu.Unlock()
		if found > 0 {
			n.broadcast("tasks")
		}
	}
}

func min(a, b int) int { if a < b { return a }; return b }

// autoMiner continuously mines pending subtasks.
func (n *RealNode) autoMiner() {
	token := os.Getenv("LMSTUDIO_TOKEN")
	if token == "" {
		log.Printf("WARNING: LMSTUDIO_TOKEN env not set. AI mining disabled. Set: export LMSTUDIO_TOKEN=your_token")
	}
	trace("MINER: goroutine started, token=%s...", token[:20])

	for {
		time.Sleep(2 * time.Second)
		trace("MINER: scanning %d subtasks for Pending", len(n.subtasks))

		n.mu.Lock()
		trace("MINER: lock acquired, iterating subtasks")
		var target *SubtaskView
		var targetIdx int
		pendingCount := 0
		runningCount := 0
		completedCount := 0
		for i := range n.subtasks {
			switch n.subtasks[i].Status {
			case "Pending":
				pendingCount++
				if target == nil {
					target = &n.subtasks[i]
					targetIdx = i
				}
			case "Running":
				runningCount++
			case "Completed":
				completedCount++
			}
		}
		trace("MINER: scan results: pending=%d running=%d completed=%d total=%d", pendingCount, runningCount, completedCount, len(n.subtasks))

		if target == nil {
			trace("MINER: no Pending subtask found, releasing lock")
			n.mu.Unlock()
			continue
		}

		trace("MINER: CLAIMING subtask[%d] id=%s taskID=%s prompt='%s'", targetIdx, target.ID, target.TaskID, target.Prompt[:min(50,len(target.Prompt))])
		trace("MINER: subtask[%d] status Pending → Running", targetIdx)
		n.subtasks[targetIdx].Status = "Running"
		stID := target.ID
		parentID := target.TaskID
		prompt := target.Prompt
		n.mu.Unlock()
		n.broadcast("tasks")
		trace("MINER: lock released, starting inference for subtask %s", stID)

		// Signal frontend: mining started
		n.pushStreamToken("\n\n=== Mining subtask " + stID[:12] + " ===\n")
		n.pushStreamToken("Model: qwen3.6-35b-a3b-mtp\n")
		n.pushStreamToken("Prompt: " + prompt[:min(100,len(prompt))] + "...\n\n")

		trace("MINER: inference START model=%s prompt_len=%d", "qwen3.6-35b-a3b-mtp", len(prompt))
		startTime := time.Now()
		result, err := runRealInferenceStream("qwen3.6-35b-a3b-mtp", prompt, token, func(chunk string) {
			n.pushStreamToken(chunk)
		})
		elapsed := time.Since(startTime)
		n.pushStreamToken("\n\n=== Complete (" + elapsed.Round(time.Millisecond).String() + ") ===\n")
		trace("MINER: inference END elapsed=%v err=%v result_len=%d", elapsed, err, len(result))

		trace("MINER: acquiring lock to update subtask[%d]", targetIdx)
		n.mu.Lock()
		trace("MINER: lock acquired for result commit")

		// Re-find the subtask by ID (index might have shifted)
		found := false
		for i := range n.subtasks {
			if n.subtasks[i].ID == stID {
				targetIdx = i
				target = &n.subtasks[i]
				found = true
				break
			}
		}
		if !found {
			trace("MINER: ERROR subtask %s not found after inference!", stID)
			n.mu.Unlock()
			continue
		}

		if err != nil {
			trace("MINER: subtask[%d] status Running → Failed (error: %v)", targetIdx, err)
			n.subtasks[targetIdx].Status = "Failed"
			n.subtasks[targetIdx].Result = fmt.Sprintf("Error: %v", err)
		} else {
			trace("MINER: subtask[%d] status Running → Completed (%d chars)", targetIdx, len(result))
			n.subtasks[targetIdx].Status = "Completed"
			n.subtasks[targetIdx].Result = result
		}

		// ---- Aggregation check ----
		trace("MINER: checking aggregation for parent task %s", parentID)
		var subtaskStates []string
		allDone := true
		var results []string
		for _, st := range n.subtasks {
			if st.TaskID == parentID {
				subtaskStates = append(subtaskStates, fmt.Sprintf("st[%d]=%s", st.Index, st.Status))
				if st.Status == "Pending" || st.Status == "Running" {
					allDone = false
					trace("MINER:   subtask[%d] still %s → NOT all done", st.Index, st.Status)
				} else {
					trace("MINER:   subtask[%d] status=%s (terminal)", st.Index, st.Status)
				}
				if st.Result != "" {
					results = append(results, fmt.Sprintf("[Step %d]\n%s", st.Index+1, st.Result))
				}
			}
		}
		trace("MINER: aggregation decision for task %s: allDone=%v states=%v", parentID, allDone, subtaskStates)

		if allDone {
			trace("MINER: all subtasks done, marking parent task %s as Completed", parentID)
			for i := range n.tasks {
				if n.tasks[i].ID == parentID {
					trace("MINER: parent task[%d] status %s → Completed, %d result parts", i, n.tasks[i].Status, len(results))
					n.tasks[i].Status = "Completed"
					n.tasks[i].Result = strings.Join(results, "\n\n")
					n.tasks[i].Subtasks = n.filterSubtasks(parentID)

					newBlock := BlockView{
						Height: uint64(len(n.blocks)),
						Hash:   fmt.Sprintf("%064x", time.Now().UnixNano()),
						Txs:    1,
						Time:   time.Now().Format("15:04:05"),
					}
					n.blocks = append(n.blocks, newBlock)
					n.transactions = append(n.transactions, TxView{
						Hash: parentID, Type: "TaskCompleted", Amount: n.tasks[i].Fee, Time: time.Now().Format("15:04:05"),
					})
					if n.mempoolSize > 0 {
						n.mempoolSize--
					}
					trace("MINER: block created height=%d hash=%s", newBlock.Height, newBlock.Hash[:16])
					break
				}
			}
		} else {
			trace("MINER: task %s NOT yet complete, waiting for remaining subtasks", parentID)
		}
		trace("MINER: releasing lock after commit")
		n.mu.Unlock()
		n.broadcast("tasks")
		n.broadcast("blocks")
	}
}

// splitTask decomposes a complex task into subtasks based on its content.

func splitTask(task *TaskView) []SubtaskView {
	prompt := task.Prompt
	trace("SPLIT: task %s prompt_len=%d prompt='%s'", task.ID, len(prompt), prompt[:min(60,len(prompt))])

	multiQuestions := detectMultiQuestions(prompt)
	if len(multiQuestions) >= 2 {
		trace("SPLIT: detected %d distinct questions, splitting", len(multiQuestions))
		subs := make([]SubtaskView, len(multiQuestions))
		for i, q := range multiQuestions {
			subs[i] = SubtaskView{
				ID:     fmt.Sprintf("st-%x", sha256.Sum256([]byte(fmt.Sprintf("%s-%d", task.ID, i))))[:20],
				TaskID: task.ID,
				Index:  i,
				Prompt: q,
				Status: "Pending",
			}
		}
		return subs
	}

	trace("SPLIT: single question, 1 subtask = original prompt")
	return []SubtaskView{{
		ID:     fmt.Sprintf("st-%x", sha256.Sum256([]byte(task.ID+"0")))[:20],
		TaskID: task.ID,
		Index:  0,
		Prompt: prompt,
		Status: "Pending",
	}}
}

func detectMultiQuestions(prompt string) []string {
	qCount := strings.Count(prompt, "?")
	if qCount >= 2 {
		parts := strings.Split(prompt, "?")
		result := make([]string, 0)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p+"?")
			}
		}
		if len(result) >= 2 {
			return result
		}
	}

	keywords := []string{". Also ", ". Then ", ". Additionally ", ". Next ", ". Finally "}
	for _, kw := range keywords {
		if strings.Contains(prompt, kw) {
			parts := strings.SplitN(prompt, kw, 5)
			result := make([]string, 0)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			if len(result) >= 2 {
				return result
			}
		}
	}

	return nil
}

// ---- WebSocket ----

func (n *RealNode) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "not supported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 16)
	n.mu.Lock()
	n.wsClients = append(n.wsClients, ch)
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()
		for i, c := range n.wsClients {
			if c == ch {
				n.wsClients = append(n.wsClients[:i], n.wsClients[i+1:]...)
				break
			}
		}
		n.mu.Unlock()
	}()

	for {
		select {
		case evt := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", evt)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (n *RealNode) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 256)
	n.mu.Lock()
	n.liveStream = append(n.liveStream, ch)
	trace("STREAM: client connected, now %d viewers", len(n.liveStream))
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()
		for i, c := range n.liveStream {
			if c == ch {
				n.liveStream = append(n.liveStream[:i], n.liveStream[i+1:]...)
				break
			}
		}
		trace("STREAM: client disconnected, %d viewers remain", len(n.liveStream))
		n.mu.Unlock()
	}()

	for {
		select {
		case token := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", token)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (n *RealNode) pushStreamToken(token string) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, ch := range n.liveStream {
		select {
		case ch <- token:
		default:
		}
	}
}

func (n *RealNode) broadcast(event string) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, ch := range n.wsClients {
		select {
		case ch <- event:
		default:
		}
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ---- Real Inference ----

// runRealInferenceStream calls LM Studio with streaming, calling onChunk for each token.
func runRealInferenceStream(model, prompt, token string, onChunk func(string)) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model, "max_tokens": 3000, "temperature": 0.7, "stream": true,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a direct assistant. Answer the user's question directly without showing your thinking process. Do NOT use reasoning_content. Output your final answer immediately in the content field. Be thorough and complete."},
			{"role": "user", "content": prompt},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:1234/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LM Studio unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LM Studio %d", resp.StatusCode)
	}

	var fullText strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			d := chunk.Choices[0].Delta
			text := d.Content
			if text == "" {
				text = d.ReasoningContent
			}
			if text != "" {
				fullText.WriteString(text)
				onChunk(text)
			}
		}
	}
	return fullText.String(), scanner.Err()
}

func runRealInference(model, prompt, token string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model, "max_tokens": 3000, "temperature": 0.7, "stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a direct assistant. Answer the user's question directly without showing your thinking process. Output your final answer immediately. Be thorough."},
			{"role": "user", "content": prompt},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:1234/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LM Studio unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LM Studio %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct{ TotalTokens int `json:"total_tokens"` } `json:"usage"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	c := result.Choices[0].Message.Content
	reasoning := result.Choices[0].Message.ReasoningContent

	// Qwen reasoning model: thinking goes to reasoning_content, final answer to content
	// If content is empty but reasoning exists, the model was still thinking when max_tokens hit
	if c == "" && reasoning != "" {
		c = "[Model Thinking Process]\n" + reasoning + "\n\n[Note: model ran out of tokens before producing final answer. Try a simpler prompt or increase max_tokens.]"
	} else if reasoning != "" {
		c = "[Thinking]\n" + reasoning + "\n\n[Answer]\n" + c
	}

	return fmt.Sprintf("%s\n[%d tokens, LM Studio]", c, result.Usage.TotalTokens), nil
}

// ---- Hardware Detection ----

func detectHardware() (gpuName string, gpuCount int, vramMB, ramMB uint64, models []string) {
	gpuName = "CPU-only"
	gpuCount = 1
	entries, _ := os.ReadDir("/sys/class/drm")
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "card") && !strings.Contains(name, "render") {
			gpuCount++
			for _, p := range []string{
				fmt.Sprintf("/sys/class/drm/%s/device/mem_info_vram_total", name),
				fmt.Sprintf("/sys/class/drm/%s/device/mem_info_vis_vram_total", name),
			} {
				if data, err := os.ReadFile(p); err == nil {
					fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &vramMB)
					vramMB /= 1024 * 1024
				}
			}
		}
	}
	if gpuCount > 1 {
		gpuCount--
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					gpuName = strings.TrimSpace(parts[1])
				}
				break
			}
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				var kb uint64
				fmt.Sscanf(strings.Fields(line)[1], "%d", &kb)
				ramMB = kb / 1024
				break
			}
		}
	}
	if vramMB < 1024 && ramMB > 0 {
		vramMB = ramMB / 2
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1234/v1/models", nil)
	token := os.Getenv("LMSTUDIO_TOKEN")
	if token == "" {
		return // no models listed if token not set
	}
	req.Header.Set("Authorization", "Bearer "+token)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if resp, err := http.DefaultClient.Do(req.WithContext(ctx)); err == nil {
		defer resp.Body.Close()
		var r struct{ Data []struct{ ID string `json:"id"` } `json:"data"` }
		if json.NewDecoder(resp.Body).Decode(&r) == nil {
			for _, m := range r.Data {
				models = append(models, m.ID)
			}
		}
	}
	return
}
