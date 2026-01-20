package memory

import (
	"fmt"
	"os"
	"testing"
	"time"

	memobase "github.com/memodb-io/memobase/src/client/memobase-go/core"
)

// func TestMemobaseClient_AddUser(t *testing.T) {
// 	oldUserID := uuid.New().String()
// 	client, err := memobase.NewMemoBaseClient("https://memobase-server-api.dpanel-c-memobase.orb.local", "example")
// 	if err != nil {
// 		t.Fatalf("Failed to create Memobase client: %v", err)
// 	}

// 	//89aa4b19-6b48-c8a1-3a65-49bb1eaebd80
// 	fmt.Println(oldUserID)
// 	t.Logf("oldUserID: %s", oldUserID)
// 	userID, err := client.AddUser(map[string]interface{}{
// 		"name": "test-user",
// 	}, oldUserID)
// 	if err != nil {
// 		t.Fatalf("Failed to add user: %v", err)
// 	}

// 	t.Logf("User ID: %s", userID)
// }

func TestMemobaseClient_GetUser(t *testing.T) {
	if os.Getenv("ENABLE_MEMOBASE_TEST") != "true" {
		t.Skip("memobase integration test disabled")
	}
	//address := "https://memobase-server-api.dpanel-c-memobase.orb.local"
	address := "http://localhost:8777"
	client, err := memobase.NewMemoBaseClient(address, "example")
	if err != nil {
		t.Fatalf("Failed to create Memobase client: %v", err)
	}

	user, err := client.GetUser("3b082fed-ac2f-5881-ad64-6b9cfcab05a9", false)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	fmt.Println(user)

	ct := time.Now()
	c, err := user.Context(nil)
	if err != nil {
		t.Fatalf("Failed to get context: %v", err)
	}
	fmt.Println(time.Since(ct))

	fmt.Println("############Context")
	fmt.Println(c)

	fmt.Println("############Profile")
	pt := time.Now()
	p, err := user.Profile(nil)
	if err != nil {
		t.Fatalf("Failed to get context: %v", err)
	}
	fmt.Println(time.Since(pt))
	fmt.Printf("%+v\n", p)

}
