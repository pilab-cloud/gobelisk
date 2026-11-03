package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/spf13/cobra"
)

var webPort int

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "start web dial pad bridge",
	Run: func(cmd *cobra.Command, args []string) {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, dialPadHTML)
		})
		http.HandleFunc("/api/dial", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			client, conn, err := dialRPC()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer conn.Close()
			resp, err := client.OriginateCall(withAuth(r.Context()), &gobeliskv1.OriginateCallRequest{From: req.From, To: req.To})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"message": resp.Message})
		})
		addr := fmt.Sprintf(":%d", webPort)
		fmt.Println("Web dial pad on http://localhost" + addr)
		http.ListenAndServe(addr, nil)
	},
}

const dialPadHTML = `<!DOCTYPE html><html><body>
<h1>Dial Pad</h1>
<input id="from" placeholder="from ext" />
<input id="to" placeholder="to ext" />
<br/>
<button onclick="dial()">Dial</button>
<script>
function dial(){
  fetch('/api/dial',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({from:document.getElementById('from').value,to:document.getElementById('to').value})})
  .then(r=>r.json()).then(d=>alert(d.message));
}
</script>
</body></html>`

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 8088, "web port")
	rootCmd.AddCommand(webCmd)
}