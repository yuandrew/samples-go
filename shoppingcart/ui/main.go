package main

import (
	"context"
	"fmt"
	"github.com/pborman/uuid"
	"github.com/temporalio/samples-go/shoppingcart"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"log"
	"net/http"
	"sort"
)

var (
	workflowClient client.Client
	// Units are in cents
	itemCosts = map[string]int{
		"apple":      200,
		"banana":     100,
		"watermelon": 500,
		"television": 100000,
		"house":      100000000,
		"car":        5000000,
		"binder":     1000,
	}
	sessionId = newSession()
)

func main() {
	var err error
	workflowClient, err = client.Dial(client.Options{
		HostPort: client.DefaultHostPort,
	})
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/", listHandler)
	http.HandleFunc("/action", actionHandler)

	fmt.Println("Shopping Cart UI available at http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func listHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html") // Set the content type to HTML
	_, _ = fmt.Fprint(w, "<h1>SAMPLE SHOPPING WEBSITE</h1>"+
		"<a href=\"/list\">HOME</a> <a href=\"/action?type=checkout\">Checkout</a>"+
		"<h3>Available Items to Purchase</h3><table border=1><tr><th>Item</th><th>Cost</th><th>Action</th>")

	keys := make([]string, 0)
	for k := range itemCosts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		actionButton := fmt.Sprintf("<a href=\"/action?type=add&itemID=%s\">"+
			"<button style=\"background-color:#4CAF50;\">Add to Cart</button></a>", k)
		dollars := float64(itemCosts[k]) / 100
		_, _ = fmt.Fprintf(w, "<tr><td>%s</td><td>$%.2f</td><td>%s</td></tr>", k, dollars, actionButton)
	}
	_, _ = fmt.Fprint(w, "</table><h3>Current items in cart:</h3>"+
		"<table border=1><tr><th>Item</th><th>Quantity</th><th>Action</th>")

	cartState := updateWithStartCart("list", "")

	// List current items in cart
	keys = make([]string, 0)
	for k := range cartState.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		removeButton := fmt.Sprintf("<a href=\"/action?type=remove&itemID=%s\">"+
			"<button style=\"background-color:#f44336;\">Remove Item</button></a>", k)
		_, _ = fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td><td>%s</td></tr>", k, cartState.Items[k], removeButton)
	}
	_, _ = fmt.Fprint(w, "</table>")
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	actionType := r.URL.Query().Get("type")
	switch actionType {
	case "checkout":
		err := workflowClient.SignalWorkflow(context.Background(), sessionId, "", "checkout", nil)
		if err != nil {
			log.Fatalln("Error signaling checkout:", err)
		}
	case "add", "remove", "list":
		itemID := r.URL.Query().Get("itemID")
		updateWithStartCart(actionType, itemID)
	default:
		log.Fatalln("Invalid action type:", actionType)
	}

	// Generate the HTML after communicating with the Temporal workflow.
	// "list" already generates HTML, so skip for that scenario
	if actionType != "list" {
		listHandler(w, r)
	}
}

func updateWithStartCart(actionType string, itemID string) shoppingcart.CartState {
	// Handle a client request to add an item to the shopping cart. The user is not logged in, but a session ID is
	// available from a cookie, and we use this as the cart ID. The Temporal client was created at service-start
	// time and is shared by all request handlers.
	//
	// A Workflow Type exists that can be used to represent a shopping cart. The method uses update-with-start to
	// add an item to the shopping cart, creating the cart if it doesn't already exist.
	//
	// Note that the workflow handle is available, even if the Update fails.
	ctx := context.Background()

	updateWithStartOptions := client.UpdateWithStartWorkflowOptions{
		StartWorkflowOperation: workflowClient.NewWithStartWorkflowOperation(client.StartWorkflowOptions{
			ID:        sessionId,
			TaskQueue: shoppingcart.TaskQueueName,
			// WorkflowIDConflictPolicy is required when using UpdateWithStartWorkflow.
			// Here we use USE_EXISTING, because we want to reuse the running workflow, as it
			// is long-running and keeping track of our cart state.
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		}, shoppingcart.CartWorkflow, nil),
		UpdateOptions: client.UpdateWorkflowOptions{
			UpdateName:   shoppingcart.UpdateName,
			WaitForStage: client.WorkflowUpdateStageCompleted,
			Args:         []interface{}{actionType, itemID},
		},
	}
	updateHandle, err := workflowClient.UpdateWithStartWorkflow(ctx, updateWithStartOptions)
	if err != nil {
		// For example, a client-side validation error (e.g. missing conflict
		// policy or invalid workflow argument types in the start operation), or
		// a server-side failure (e.g. failed to start workflow, or exceeded
		// limit on concurrent update per workflow execution).
		log.Fatalln("Error issuing update-with-start:", err)
	}

	log.Println("Updated workflow",
		"WorkflowID:", updateHandle.WorkflowID(),
		"RunID:", updateHandle.RunID())

	// Always use a zero variable before calling Get for any Go SDK API
	cartState := shoppingcart.CartState{Items: make(map[string]int)}
	if err = updateHandle.Get(ctx, &cartState); err != nil {
		log.Fatalln("Error obtaining update result:", err)
	}
	return cartState
}

func newSession() string {
	return "session-" + uuid.New()
}
