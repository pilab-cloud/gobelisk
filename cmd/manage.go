package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var rpcAddr string
var rpcToken string

func dialRPC() (gobeliskv1.ManagementClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(rpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	client := gobeliskv1.NewManagementClient(conn)
	return client, conn, nil
}

func withAuth(ctx context.Context) context.Context {
	if rpcToken != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+rpcToken)
	}
	return ctx
}

var manageCmd = &cobra.Command{
	Use:   "manage",
	Short: "manage gobelisk server over gRPC",
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "show server status",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.Status(withAuth(context.Background()), &gobeliskv1.StatusRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Printf("host: %s\nport: %d\n", resp.Host, resp.Port)
	},
}

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "manage users",
}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.ListUsers(withAuth(context.Background()), &gobeliskv1.ListUsersRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		for u, p := range resp.Users {
			fmt.Printf("%s: %s\n", u, p)
		}
	},
}

var usersAddCmd = &cobra.Command{
	Use:   "add <user> <password>",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.AddUser(withAuth(context.Background()), &gobeliskv1.AddUserRequest{User: args[0], Password: args[1]})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var usersRemoveCmd = &cobra.Command{
	Use:   "remove <user>",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.RemoveUser(withAuth(context.Background()), &gobeliskv1.RemoveUserRequest{User: args[0]})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "manage config",
}

var configGetCmd = &cobra.Command{
	Use: "get",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.GetConfig(withAuth(context.Background()), &gobeliskv1.GetConfigRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Yaml)
	},
}

var configSetCmd = &cobra.Command{
	Use: "set <file>",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.SetConfig(withAuth(context.Background()), &gobeliskv1.SetConfigRequest{Yaml: string(data)})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var configReloadCmd = &cobra.Command{
	Use: "reload",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.ReloadConfig(withAuth(context.Background()), &gobeliskv1.ReloadConfigRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var dialplanCmd = &cobra.Command{
	Use:   "dialplan",
	Short: "manage dialplan",
}

var dialplanGetCmd = &cobra.Command{
	Use: "get",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.GetDialPlan(withAuth(context.Background()), &gobeliskv1.GetDialPlanRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Text)
	},
}

var dialplanSetCmd = &cobra.Command{
	Use: "set <file>",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.SetDialPlan(withAuth(context.Background()), &gobeliskv1.SetDialPlanRequest{Text: string(data)})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var extensionsCmd = &cobra.Command{
	Use:   "extensions",
	Short: "manage extensions",
}

var extensionsListCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.ListExtensions(withAuth(context.Background()), &gobeliskv1.ListExtensionsRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		for ext, contact := range resp.Extensions {
			fmt.Printf("%s: %s\n", ext, contact)
		}
	},
}

var extensionsGetCmd = &cobra.Command{
	Use:  "get <extension>",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.GetExtension(withAuth(context.Background()), &gobeliskv1.GetExtensionRequest{Extension: args[0]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Contact)
	},
}

var extensionsCreateCmd = &cobra.Command{
	Use:  "create <extension> <contact>",
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.CreateExtension(withAuth(context.Background()), &gobeliskv1.CreateExtensionRequest{Extension: args[0], Contact: args[1]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}

var extensionsUpdateCmd = &cobra.Command{
	Use:  "update <extension> <contact>",
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.UpdateExtension(withAuth(context.Background()), &gobeliskv1.UpdateExtensionRequest{Extension: args[0], Contact: args[1]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}

var roomsCmd = &cobra.Command{Use: "rooms", Short: "manage rooms"}
var roomsListCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.ListRooms(withAuth(context.Background()), &gobeliskv1.ListRoomsRequest{})
		if err != nil { fmt.Println("error:", err); return }
		for _, r := range resp.Rooms { fmt.Println(r) }
	},
}
var roomsCreateCmd = &cobra.Command{
	Use: "create <name>", Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.CreateRoom(withAuth(context.Background()), &gobeliskv1.CreateRoomRequest{Name: args[0]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}
var roomsRemoveCmd = &cobra.Command{
	Use: "remove <name>", Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.RemoveRoom(withAuth(context.Background()), &gobeliskv1.RemoveRoomRequest{Name: args[0]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}

var meetingsCmd = &cobra.Command{Use: "meetings", Short: "manage meetings"}
var meetingsListCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.ListMeetings(withAuth(context.Background()), &gobeliskv1.ListMeetingsRequest{})
		if err != nil { fmt.Println("error:", err); return }
		for k, v := range resp.Meetings { fmt.Printf("%s: %s\n", k, v) }
	},
}
var meetingsCreateCmd = &cobra.Command{
	Use: "create <name> <extension>", Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.CreateMeeting(withAuth(context.Background()), &gobeliskv1.CreateMeetingRequest{Name: args[0], Extension: args[1]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}
var meetingsRemoveCmd = &cobra.Command{
	Use: "remove <name>", Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil { fmt.Println("error:", err); return }
		defer conn.Close()
		resp, err := client.RemoveMeeting(withAuth(context.Background()), &gobeliskv1.RemoveMeetingRequest{Name: args[0]})
		if err != nil { fmt.Println("error:", err); return }
		fmt.Println(resp.Message)
	},
}

var extensionsRemoveCmd = &cobra.Command{
	Use:  "remove <extension>",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.RemoveExtension(withAuth(context.Background()), &gobeliskv1.RemoveExtensionRequest{Extension: args[0]})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(resp.Message)
	},
}

var callsCmd = &cobra.Command{
	Use:   "calls",
	Short: "manage calls",
}

var callsListCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {
		client, conn, err := dialRPC()
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		defer conn.Close()
		resp, err := client.ListCalls(withAuth(context.Background()), &gobeliskv1.ListCallsRequest{})
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		for _, c := range resp.CallIds {
			fmt.Println(c)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rpcAddr, "rpc-addr", "127.0.0.1:9090", "gRPC server address")
	rootCmd.PersistentFlags().StringVar(&rpcToken, "rpc-token", "", "gRPC bearer token")

	manageCmd.AddCommand(statusCmd)
	usersCmd.AddCommand(usersListCmd, usersAddCmd, usersRemoveCmd)
	configCmd.AddCommand(configGetCmd, configSetCmd, configReloadCmd)
	dialplanCmd.AddCommand(dialplanGetCmd, dialplanSetCmd)
	callsCmd.AddCommand(callsListCmd)
	extensionsCmd.AddCommand(extensionsListCmd, extensionsGetCmd, extensionsCreateCmd, extensionsUpdateCmd, extensionsRemoveCmd)
	roomsCmd.AddCommand(roomsListCmd, roomsCreateCmd, roomsRemoveCmd)
	meetingsCmd.AddCommand(meetingsListCmd, meetingsCreateCmd, meetingsRemoveCmd)
	manageCmd.AddCommand(usersCmd, configCmd, dialplanCmd, callsCmd, extensionsCmd, roomsCmd, meetingsCmd)
	rootCmd.AddCommand(manageCmd)
}

