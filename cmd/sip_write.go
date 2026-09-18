package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// Write commands for the SIP Trunking objects. Reads live in sip.go.

const (
	dirInbound  = "inbound"
	dirOutbound = "outbound"
)

// ─── helpers shared by the write verbs ───────────────────────────────────────

// listTrunks fetches every trunk, so a delete can say what it would break.
// Best-effort: a failure returns nil and the caller skips the preview rather
// than blocking a legitimate delete on a read it does not strictly need.
func listTrunks(client *api.Client) []api.SIPTrunk {
	q := url.Values{}
	q.Set("limit", "20")
	var all []api.SIPTrunk
	for offset := 0; offset < 200; offset += 20 {
		q.Set("offset", strconv.Itoa(offset))
		var resp api.SIPTrunkList
		apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Trunk"), nil, q, &resp)
		if err != nil || apiErr != nil {
			return all
		}
		all = append(all, resp.Objects...)
		if len(resp.Objects) < 20 {
			break
		}
	}
	return all
}

// trunksReferencing names every trunk pointing at uuid, so the user sees what a
// delete would detach before it happens rather than after.
func trunksReferencing(trunks []api.SIPTrunk, uuid string) []string {
	var out []string
	for _, t := range trunks {
		switch uuid {
		case t.PrimaryURIUUID:
			out = append(out, t.TrunkID+" ("+t.Name+", primary URI)")
		case t.FallbackURIUUID:
			out = append(out, t.TrunkID+" ("+t.Name+", fallback URI)")
		case t.CredentialUUID:
			out = append(out, t.TrunkID+" ("+t.Name+", credential)")
		case t.IPACLUUID:
			out = append(out, t.TrunkID+" ("+t.Name+", IP ACL)")
		}
	}
	return out
}

// numbersOnTrunk counts numbers routed to a trunk. A number carries its trunk in
// `application` as a Zentrunk resource path, not in a trunk_id field.
func numbersOnTrunk(client *api.Client, trunkID string) int {
	q := url.Values{}
	q.Set("limit", "20")
	marker := "/Zentrunk/Trunk/" + trunkID + "/"
	count := 0
	for offset := 0; offset < 400; offset += 20 {
		q.Set("offset", strconv.Itoa(offset))
		var resp api.NumberList
		apiErr, err := client.Do("GET", client.AccountURL("Number"), nil, q, &resp)
		if err != nil || apiErr != nil {
			return count
		}
		for _, n := range resp.Objects {
			if strings.Contains(n.Application, marker) {
				count++
			}
		}
		if len(resp.Objects) < 20 {
			break
		}
	}
	return count
}

// confirmDestructive refuses without --yes, naming what would be affected.
func confirmDestructive(action string, affected []string, extra string) error {
	if yesFlag {
		return nil
	}
	reportDependents(affected)
	if extra != "" {
		fmt.Fprintf(os.Stderr, "%s\n", extra)
	}
	return clierr.DestructiveRefused(action)
}

// reportDependents names what a delete would detach. Printed on every delete,
// confirmed or not: with --yes there is no prompt to carry the warning.
func reportDependents(affected []string) {
	if len(affected) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "In use by %d trunk(s):\n", len(affected))
	for _, a := range affected {
		fmt.Fprintf(os.Stderr, "  - %s\n", a)
	}
}

// boolFlagPatch adds a boolean only when the user actually passed it, so an
// unset flag never silently rewrites a stored value. This is what makes
// --secure=false able to turn something off rather than read as "not given".
func boolFlagPatch(cmd *cobra.Command, flag, field string, val bool, body map[string]any) {
	if cmd.Flags().Changed(flag) {
		body[field] = val
	}
}

func postSIP(client *api.Client, body map[string]any, parts ...string) (map[string]any, error) {
	var out map[string]any
	apiErr, err := client.Do("POST", client.AccountURL(parts...), body, nil, &out)
	if err != nil {
		return nil, err
	}
	if apiErr != nil {
		return nil, apiErr
	}
	return out, nil
}

func deleteSIP(client *api.Client, parts ...string) error {
	apiErr, err := client.Do("DELETE", client.AccountURL(parts...), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	return nil
}

// emitWrite renders the result of an update or delete. These used to print
// prose on stderr and nothing on stdout, so `-o json` produced an empty stream
// and exit 0 — indistinguishable from success with no data to a jq pipeline.
func emitWrite(human string, fields map[string]any) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, mustJSON(fields))
	}
	fmt.Fprintf(os.Stderr, "%s\n", human)
	return nil
}

// ─── trunks: create / update / delete ────────────────────────────────────────

var (
	trunkCreateName, trunkCreateDirection   string
	trunkCreateURI, trunkCreateFallbackURI  string
	trunkCreateCredential, trunkCreateIPACL string
	trunkCreateSecure                       bool
	trunkUpdateName, trunkUpdateStatus      string
	trunkUpdateURI, trunkUpdateFallbackURI  string
	trunkUpdateCredential, trunkUpdateIPACL string
	trunkUpdateSecure                       bool
	trunkUpdateDirection                    string
)

var sipTrunksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a trunk",
	Long: `Create an inbound or outbound trunk.

An inbound trunk needs a primary URI. An outbound trunk needs a credential or an
IP access control list to authenticate your platform. Both are checked here
before the request, because the API's own error does not say which is missing.

trunk_domain is only returned on a read, so this reads the trunk back and prints
it: that domain is what you paste into your platform.`,
	Example: `  plivo sip trunks create --name my-trunk --direction inbound --uri <uri_uuid>
  plivo sip trunks create --name out --direction outbound --credential <uuid>`,
	RunE: runSIPTrunksCreate,
}

var sipTrunksUpdateCmd = &cobra.Command{
	Use:   "update <trunk_id>",
	Short: "Update a trunk",
	Long: `Update a trunk.

Boolean and state flags take an explicit value so they can be reversed:
--secure=false and --status disabled both turn something off. A flag you do not
pass is left untouched.`,
	Example: `  plivo sip trunks update <id> --status disabled
  plivo sip trunks update <id> --secure=false`,
	Args: cobra.ExactArgs(1),
	RunE: runSIPTrunksUpdate,
}

var sipTrunksDeleteCmd = &cobra.Command{
	Use:   "delete <trunk_id>",
	Short: "Delete a trunk (requires --yes)",
	Long: `Delete a trunk.

Reports how many numbers are routed to it first: deleting a trunk detaches every
one of them, and inbound calls to those numbers stop.`,
	Args: cobra.ExactArgs(1),
	RunE: runSIPTrunksDelete,
}

func runSIPTrunksCreate(cmd *cobra.Command, args []string) error {
	switch trunkCreateDirection {
	case dirInbound:
		if trunkCreateURI == "" {
			return clierr.BadInput("an inbound trunk needs --uri (the origination URI calls arrive on)")
		}
	case dirOutbound:
		if trunkCreateCredential == "" && trunkCreateIPACL == "" {
			return clierr.BadInput("an outbound trunk needs --credential or --ip-acl to authenticate your platform")
		}
	default:
		return clierr.BadInput("--direction must be inbound or outbound")
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"name": trunkCreateName, "trunk_direction": trunkCreateDirection}
	for k, v := range map[string]string{
		"primary_uri_uuid":  trunkCreateURI,
		"fallback_uri_uuid": trunkCreateFallbackURI,
		"credential_uuid":   trunkCreateCredential,
		"ipacl_uuid":        trunkCreateIPACL,
	} {
		if v != "" {
			body[k] = v
		}
	}
	boolFlagPatch(cmd, "secure", "secure", trunkCreateSecure, body)

	created, err := postSIP(client, body, "Zentrunk", "Trunk")
	if err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	id, _ := created["trunk_id"].(string)
	if id == "" {
		return output.JSONRaw(os.Stdout, mustJSON(created))
	}
	// Read back: the domain the customer needs is not on the create response.
	var t api.SIPTrunk
	if apiErr, derr := client.Do("GET", client.AccountURL("Zentrunk", "Trunk", id), nil, nil, &t); derr == nil && apiErr == nil {
		t = unwrapSIPTrunk(t)
	}
	// Merge the read-back into BOTH renderings. Returning the create response
	// alone left -o json without trunk_domain while the table showed it.
	for k, v := range map[string]string{
		"trunk_domain":     t.TrunkDomain,
		"trunk_direction":  t.TrunkDirection,
		"trunk_status":     t.TrunkStatus,
		"name":             t.Name,
		"primary_uri_uuid": t.PrimaryURIUUID,
		"credential_uuid":  t.CredentialUUID,
		"ipacl_uuid":       t.IPACLUUID,
	} {
		if v != "" {
			created[k] = v
		}
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, mustJSON(created))
	}
	fmt.Fprintf(os.Stderr, "Created trunk %s\n", id)
	return output.KV(os.Stdout, [][2]string{
		{"trunk_id", id},
		{"name", t.Name},
		{"trunk_direction", t.TrunkDirection},
		{"trunk_status", t.TrunkStatus},
		{"trunk_domain", t.TrunkDomain},
	})
}

func runSIPTrunksUpdate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	for flag, pair := range map[string][2]string{
		"name":         {"name", trunkUpdateName},
		"status":       {"trunk_status", trunkUpdateStatus},
		"uri":          {"primary_uri_uuid", trunkUpdateURI},
		"fallback-uri": {"fallback_uri_uuid", trunkUpdateFallbackURI},
		"credential":   {"credential_uuid", trunkUpdateCredential},
		"ip-acl":       {"ipacl_uuid", trunkUpdateIPACL},
	} {
		if cmd.Flags().Changed(flag) {
			body[pair[0]] = pair[1]
		}
	}
	boolFlagPatch(cmd, "secure", "secure", trunkUpdateSecure, body)
	if len(body) == 0 {
		return clierr.BadInput("nothing to update — pass at least one flag")
	}
	// The API rejects any update without trunk_direction, including one that
	// does not touch it. Carry the stored value rather than 400 on every flag.
	dir := trunkUpdateDirection
	if dir == "" {
		var derr error
		if dir, derr = trunkDirectionOf(client, args[0]); derr != nil {
			return derr
		}
	}
	body["trunk_direction"] = dir
	if _, err := postSIP(client, body, "Zentrunk", "Trunk", args[0]); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Updated trunk %s", args[0]),
		map[string]any{"trunk_id": args[0], "updated": true})
}

func runSIPTrunksDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	extra := ""
	if n := numbersOnTrunk(client, id); n > 0 {
		extra = fmt.Sprintf("%d number(s) are routed to this trunk and will be detached.", n)
		fmt.Fprintf(os.Stderr, "%s\n", extra)
	}
	if !yesFlag {
		return confirmDestructive("delete trunk "+id, nil, extra)
	}
	if err := deleteSIP(client, "Zentrunk", "Trunk", id); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Deleted trunk %s", id),
		map[string]any{"trunk_id": id, "deleted": true})
}

// ─── uris: create / list / get / update / delete ─────────────────────────────

var (
	uriCreateName, uriCreateURI, uriCreateUsername string
	uriCreateAuthNeeded, uriCreatePasswordStdin    bool
	uriUpdatePasswordStdin                         bool
	uriUpdateName, uriUpdateURI, uriUpdateUsername string
	uriUpdateAuthNeeded                            bool
	uriListLimit, uriListOffset                    int
)

var sipURIsCmd = &cobra.Command{
	Use:     "uris",
	Aliases: []string{"uri"},
	Short:   "Origination URIs",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var sipURIsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an origination URI",
	Long: `Create an origination URI.

--uri accepts host, host:port, host;transport=tcp, or sip:user@host. A missing
port is fine and is never rejected here: the platform decides the default.`,
	Example: `  plivo sip uris create --name eleven --uri sip.rtc.elevenlabs.io:5060;transport=tcp`,
	RunE:    runSIPURIsCreate,
}

var sipURIsListCmd = &cobra.Command{Use: "list", Short: "List origination URIs", RunE: runSIPURIsList}
var sipURIsGetCmd = &cobra.Command{Use: "get <uri_uuid>", Short: "Get one origination URI", Args: cobra.ExactArgs(1), RunE: runSIPURIsGet}

var sipURIsUpdateCmd = &cobra.Command{
	Use:   "update <uri_uuid>",
	Short: "Update an origination URI",
	Long:  "--authentication-needed takes a value so it reverses: --authentication-needed=false turns it off.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPURIsUpdate,
}

var sipURIsDeleteCmd = &cobra.Command{
	Use:   "delete <uri_uuid>",
	Short: "Delete an origination URI (requires --yes)",
	Long:  "Names any trunk using it as a primary or fallback URI before deleting.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPURIsDelete,
}

// normalizeSIPURI keeps whatever shape the user gave. A bare host is legal, so
// nothing here adds a port or a scheme; it only trims stray whitespace.
func normalizeSIPURI(v string) string { return strings.TrimSpace(v) }

func runSIPURIsCreate(cmd *cobra.Command, args []string) error {
	if normalizeSIPURI(uriCreateURI) == "" {
		return clierr.BadInput("--uri is required (host, host:port, host;transport=…, or sip:user@host)")
	}
	if uriCreateAuthNeeded && uriCreateUsername == "" {
		return errAuthNeedsUsername
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"name": uriCreateName, "uri": normalizeSIPURI(uriCreateURI)}
	boolFlagPatch(cmd, "authentication-needed", "authentication_needed", uriCreateAuthNeeded, body)
	if uriCreateUsername != "" {
		body["username"] = uriCreateUsername
	}
	if uriCreatePasswordStdin {
		pw, perr := readPasswordStdin()
		if perr != nil {
			return perr
		}
		body["password"] = pw
	}
	created, err := postSIP(client, body, "Zentrunk", "URI")
	if err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return printCreated(created, "uri_uuid", "Created URI")
}

func runSIPURIsList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(uriListLimit))
	q.Set("offset", strconv.Itoa(uriListOffset))
	var resp api.SIPTrunkURIList
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "URI"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"URI_UUID", "NAME", "URI", "AUTHENTICATION_NEEDED", "USERNAME"}}
	for _, u := range resp.Objects {
		rows = append(rows, []string{u.URIUUID, u.Name, u.URI, strconv.FormatBool(u.AuthenticationNeeded), u.Username})
	}
	return output.Table(os.Stdout, rows)
}

func runSIPURIsGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var u api.SIPTrunkURI
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "URI", args[0]), nil, nil, &u)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, u.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"uri_uuid", u.URIUUID},
		{"name", u.Name},
		{"uri", u.URI},
		{"authentication_needed", strconv.FormatBool(u.AuthenticationNeeded)},
		{"username", u.Username},
		{"sip_user", u.SipUser},
	})
}

func runSIPURIsUpdate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = uriUpdateName
	}
	if cmd.Flags().Changed("uri") {
		body["uri"] = normalizeSIPURI(uriUpdateURI)
	}
	if cmd.Flags().Changed("username") {
		body["username"] = uriUpdateUsername
	}
	if uriUpdatePasswordStdin {
		pw, perr := readPasswordStdin()
		if perr != nil {
			return perr
		}
		body["password"] = pw
	}
	boolFlagPatch(cmd, "authentication-needed", "authentication_needed", uriUpdateAuthNeeded, body)
	if uriUpdateAuthNeeded && cmd.Flags().Changed("authentication-needed") && uriUpdateUsername == "" {
		return errAuthNeedsUsername
	}
	if len(body) == 0 {
		return clierr.BadInput("nothing to update — pass at least one flag")
	}
	if _, err := postSIP(client, body, "Zentrunk", "URI", args[0]); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Updated URI %s", args[0]),
		map[string]any{"uri_uuid": args[0], "updated": true})
}

func runSIPURIsDelete(cmd *cobra.Command, args []string) error {
	return deleteSIPObject(cmd, args[0], "URI", "delete URI ")
}

// deleteSIPObject is the shared delete for URIs, credentials and IP ACLs: name
// every trunk pointing at the object, then refuse without --yes.
func deleteSIPObject(cmd *cobra.Command, uuid, segment, action string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	// Always read the dependents. --yes skips the confirmation, never the check:
	// the whole point is to know what a delete detaches, and that matters most
	// when nobody is there to be asked.
	used := trunksReferencing(listTrunks(client), uuid)
	reportDependents(used)
	if !yesFlag {
		return confirmDestructive(action+uuid, nil, "")
	}
	if err := deleteSIP(client, "Zentrunk", segment, uuid); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Deleted %s %s", segment, uuid),
		map[string]any{"uuid": uuid, "resource": segment, "deleted": true})
}

func printCreated(created map[string]any, idField, label string) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, mustJSON(created))
	}
	id, _ := created[idField].(string)
	fmt.Fprintf(os.Stderr, "%s %s\n", label, id)
	pairs := [][2]string{{idField, id}}
	for _, k := range []string{"name", "uri", "username", "message"} {
		if v, ok := created[k].(string); ok && v != "" {
			pairs = append(pairs, [2]string{k, v})
		}
	}
	return output.KV(os.Stdout, pairs)
}

// ─── credentials: create / list / get / update / delete ──────────────────────

var (
	credCreateName, credCreateUsername string
	credUpdateName, credUpdateUsername string
	credCreatePasswordStdin            bool
	credUpdatePasswordStdin            bool
	credListLimit, credListOffset      int
)

var sipCredsCmd = &cobra.Command{
	Use:     "credentials",
	Aliases: []string{"credential", "creds"},
	Short:   "Trunk credentials",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var sipCredsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a credential",
	Long: `Create a credential.

The password is read from stdin only. There is no --password flag: a password in
an argument lands in shell history, process listings and CI logs. It is never
echoed, never printed back, and never generated for you.`,
	Example: `  printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name c1 --username u1 --password-stdin`,
	RunE:    runSIPCredsCreate,
}

var sipCredsListCmd = &cobra.Command{Use: "list", Short: "List credentials", RunE: runSIPCredsList}
var sipCredsGetCmd = &cobra.Command{Use: "get <credential_uuid>", Short: "Get one credential", Args: cobra.ExactArgs(1), RunE: runSIPCredsGet}

var sipCredsUpdateCmd = &cobra.Command{
	Use:   "update <credential_uuid>",
	Short: "Update a credential",
	Long:  "As with create, a new password is read from stdin only.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPCredsUpdate,
}

var sipCredsDeleteCmd = &cobra.Command{
	Use:   "delete <credential_uuid>",
	Short: "Delete a credential (requires --yes)",
	Long:  "Names any trunk using it before deleting.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPCredsDelete,
}

// readPasswordStdin takes the password from stdin. Trailing newlines are
// stripped because `printf`/`echo` and a heredoc disagree about them, and a
// password that silently carries one fails to authenticate with no clue why.
func readPasswordStdin() (string, error) {
	b, err := readAllStdin()
	if err != nil {
		return "", clierr.BadInput("reading password from stdin: " + err.Error())
	}
	pw := strings.TrimRight(string(b), "\r\n")
	if pw == "" {
		return "", clierr.BadInput("no password on stdin — pipe one in, e.g. `printf '%s' \"$PW\" | plivo sip credentials create … --password-stdin`")
	}
	return pw, nil
}

func runSIPCredsCreate(cmd *cobra.Command, args []string) error {
	if !credCreatePasswordStdin {
		return clierr.BadInput("--password-stdin is required: the password is only ever read from stdin")
	}
	if credCreateUsername == "" {
		return clierr.BadInput("--username is required")
	}
	pw, err := readPasswordStdin()
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	created, err := postSIP(client, map[string]any{
		"name": credCreateName, "username": credCreateUsername, "password": pw,
	}, "Zentrunk", "Credential")
	if err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return printCreated(created, "credential_uuid", "Created credential")
}

func runSIPCredsList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(credListLimit))
	q.Set("offset", strconv.Itoa(credListOffset))
	var resp api.SIPTrunkCredentialList
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Credential"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"CREDENTIAL_UUID", "NAME", "USERNAME"}}
	for _, c := range resp.Objects {
		rows = append(rows, []string{c.CredentialUUID, c.Name, c.Username})
	}
	return output.Table(os.Stdout, rows)
}

func runSIPCredsGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.SIPTrunkCredential
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Credential", args[0]), nil, nil, &c)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, c.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"credential_uuid", c.CredentialUUID},
		{"name", c.Name},
		{"username", c.Username},
	})
}

func runSIPCredsUpdate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = credUpdateName
	}
	if cmd.Flags().Changed("username") {
		body["username"] = credUpdateUsername
	}
	if credUpdatePasswordStdin {
		pw, err := readPasswordStdin()
		if err != nil {
			return err
		}
		body["password"] = pw
		// The API rejects a password with no username, so rotating a password
		// alone is impossible on the wire. Carry the stored username across
		// rather than making the user restate something that is not changing.
		if _, ok := body["username"]; !ok {
			var cur api.SIPTrunkCredential
			apiErr, gerr := client.Do("GET", client.AccountURL("Zentrunk", "Credential", args[0]), nil, nil, &cur)
			if gerr != nil || apiErr != nil || cur.Username == "" {
				return clierr.BadInput(
					"could not read the current username, which the API requires alongside a password — pass --username too")
			}
			body["username"] = cur.Username
		}
	}
	if len(body) == 0 {
		return clierr.BadInput("nothing to update — pass at least one flag")
	}
	if !credUpdatePasswordStdin {
		return clierr.BadInput(
			"--password-stdin is required: the API rewrites the password on every credential update, " +
				"so an update without one would blank it")
	}
	if _, err := postSIP(client, body, "Zentrunk", "Credential", args[0]); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Updated credential %s", args[0]),
		map[string]any{"credential_uuid": args[0], "updated": true})
}

func runSIPCredsDelete(cmd *cobra.Command, args []string) error {
	return deleteSIPObject(cmd, args[0], "Credential", "delete credential ")
}

// ─── ip-acl: create / update / delete ────────────────────────────────────────

var (
	aclCreateName string
	aclCreateIPs  []string
	aclUpdateName string
	aclUpdateIPs  []string
)

var sipACLCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an IP access control list",
	Long: `Create an IP access control list.

--ip is repeatable. A range that allows the whole internet is reported but not
blocked: it is occasionally deliberate, and refusing it outright would push
people to the console instead.`,
	Example: `  plivo sip ip-acl create --name platform --ip 203.0.113.4 --ip 198.51.100.0/24`,
	RunE:    runSIPACLCreate,
}

var sipACLUpdateCmd = &cobra.Command{
	Use:   "update <ipacl_uuid>",
	Short: "Update an IP access control list",
	Long:  "--ip replaces the whole list rather than appending, so pass every address you want kept.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPACLUpdate,
}

var sipACLDeleteCmd = &cobra.Command{
	Use:   "delete <ipacl_uuid>",
	Short: "Delete an IP access control list (requires --yes)",
	Long:  "Names any trunk using it before deleting.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPACLDelete,
}

// riskyCIDR reports a range that allows far more than a platform needs. It
// warns and does not block: the customer may genuinely want it, and a hard
// refusal just moves the work to the console where nothing warns at all.
func riskyCIDR(entry string) string {
	e := strings.TrimSpace(entry)
	if e == "0.0.0.0/0" || e == "::/0" {
		return e + " allows every address on the internet"
	}
	_, bits, ok := strings.Cut(e, "/")
	if !ok {
		return ""
	}
	prefix, err := strconv.Atoi(bits)
	if err != nil {
		return ""
	}
	if !strings.Contains(e, ":") && prefix < 8 {
		return fmt.Sprintf("%s is wider than /8 (%d addresses)", e, 1<<(32-prefix))
	}
	return ""
}

func warnRiskyIPs(ips []string) {
	for _, ip := range ips {
		if msg := riskyCIDR(ip); msg != "" {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)
		}
	}
}

func runSIPACLCreate(cmd *cobra.Command, args []string) error {
	if len(aclCreateIPs) == 0 {
		return clierr.BadInput("at least one --ip is required")
	}
	warnRiskyIPs(aclCreateIPs)
	client, _, err := getClient()
	if err != nil {
		return err
	}
	created, err := postSIP(client, map[string]any{
		"name": aclCreateName, "ip_addresses": aclCreateIPs,
	}, "Zentrunk", "IPAccessControlList")
	if err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return printCreated(created, "ipacl_uuid", "Created IP access control list")
}

func runSIPACLUpdate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = aclUpdateName
	}
	if cmd.Flags().Changed("ip") {
		warnRiskyIPs(aclUpdateIPs)
		body["ip_addresses"] = aclUpdateIPs
	}
	if len(body) == 0 {
		return clierr.BadInput("nothing to update — pass at least one flag")
	}
	if _, err := postSIP(client, body, "Zentrunk", "IPAccessControlList", args[0]); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Updated IP access control list %s", args[0]),
		map[string]any{"ipacl_uuid": args[0], "updated": true})
}

func runSIPACLDelete(cmd *cobra.Command, args []string) error {
	return deleteSIPObject(cmd, args[0], "IPAccessControlList", "delete IP access control list ")
}

func init() {
	cf := sipTrunksCreateCmd.Flags()
	cf.StringVar(&trunkCreateName, "name", "", "trunk name")
	cf.StringVar(&trunkCreateDirection, "direction", "", "inbound|outbound (required)")
	cf.StringVar(&trunkCreateURI, "uri", "", "primary origination URI uuid (inbound)")
	cf.StringVar(&trunkCreateFallbackURI, "fallback-uri", "", "fallback origination URI uuid")
	cf.StringVar(&trunkCreateCredential, "credential", "", "credential uuid (outbound)")
	cf.StringVar(&trunkCreateIPACL, "ip-acl", "", "IP access control list uuid (outbound)")
	cf.BoolVar(&trunkCreateSecure, "secure", false, "enable TLS/SRTP")

	uf := sipTrunksUpdateCmd.Flags()
	uf.StringVar(&trunkUpdateName, "name", "", "trunk name")
	uf.StringVar(&trunkUpdateStatus, "status", "", "enabled|disabled")
	uf.StringVar(&trunkUpdateURI, "uri", "", "primary origination URI uuid")
	uf.StringVar(&trunkUpdateFallbackURI, "fallback-uri", "", "fallback origination URI uuid")
	uf.StringVar(&trunkUpdateCredential, "credential", "", "credential uuid")
	uf.StringVar(&trunkUpdateIPACL, "ip-acl", "", "IP access control list uuid")
	uf.BoolVar(&trunkUpdateSecure, "secure", false, "enable TLS/SRTP (takes a value: --secure=false)")
	uf.StringVar(&trunkUpdateDirection, "direction", "", "inbound|outbound (read from the trunk when omitted)")

	ucf := sipURIsCreateCmd.Flags()
	ucf.StringVar(&uriCreateName, "name", "", "URI name")
	ucf.StringVar(&uriCreateURI, "uri", "", "host, host:port, host;transport=…, or sip:user@host")
	ucf.StringVar(&uriCreateUsername, "username", "", "username when authentication is needed")
	ucf.BoolVar(&uriCreatePasswordStdin, "password-stdin", false, "read the URI password from stdin")
	ucf.BoolVar(&uriCreateAuthNeeded, "authentication-needed", false, "require authentication")
	sipURIsListCmd.Flags().IntVar(&uriListLimit, "limit", 20, "rows to return")
	sipURIsListCmd.Flags().IntVar(&uriListOffset, "offset", 0, "rows to skip")
	uuf := sipURIsUpdateCmd.Flags()
	uuf.StringVar(&uriUpdateName, "name", "", "URI name")
	uuf.StringVar(&uriUpdateURI, "uri", "", "origination URI")
	uuf.StringVar(&uriUpdateUsername, "username", "", "username")
	uuf.BoolVar(&uriUpdateAuthNeeded, "authentication-needed", false, "require authentication (takes a value)")
	uuf.BoolVar(&uriUpdatePasswordStdin, "password-stdin", false, "read a new URI password from stdin")

	ccf := sipCredsCreateCmd.Flags()
	ccf.StringVar(&credCreateName, "name", "", "credential name")
	ccf.StringVar(&credCreateUsername, "username", "", "SIP username (required)")
	ccf.BoolVar(&credCreatePasswordStdin, "password-stdin", false, "read the password from stdin (required)")
	sipCredsListCmd.Flags().IntVar(&credListLimit, "limit", 20, "rows to return")
	sipCredsListCmd.Flags().IntVar(&credListOffset, "offset", 0, "rows to skip")
	cuf := sipCredsUpdateCmd.Flags()
	cuf.StringVar(&credUpdateName, "name", "", "credential name")
	cuf.StringVar(&credUpdateUsername, "username", "", "SIP username")
	cuf.BoolVar(&credUpdatePasswordStdin, "password-stdin", false, "read the password from stdin (required: every update rewrites it)")

	acf := sipACLCreateCmd.Flags()
	acf.StringVar(&aclCreateName, "name", "", "list name")
	acf.StringArrayVar(&aclCreateIPs, "ip", nil, "IP or CIDR (repeatable)")
	auf := sipACLUpdateCmd.Flags()
	auf.StringVar(&aclUpdateName, "name", "", "list name")
	auf.StringArrayVar(&aclUpdateIPs, "ip", nil, "IP or CIDR (repeatable; replaces the list)")

	sipTrunksCmd.AddCommand(sipTrunksCreateCmd, sipTrunksUpdateCmd, sipTrunksDeleteCmd)
	sipURIsCmd.AddCommand(sipURIsCreateCmd, sipURIsListCmd, sipURIsGetCmd, sipURIsUpdateCmd, sipURIsDeleteCmd)
	sipCredsCmd.AddCommand(sipCredsCreateCmd, sipCredsListCmd, sipCredsGetCmd, sipCredsUpdateCmd, sipCredsDeleteCmd)
	sipACLCmd.AddCommand(sipACLCreateCmd, sipACLUpdateCmd, sipACLDeleteCmd)
	sipCmd.AddCommand(sipURIsCmd, sipCredsCmd)
}

// mustJSON re-marshals a decoded body for the raw JSON path. The create
// responses are small and already parsed, so a failure here is not possible in
// practice; an empty object is still valid JSON if it ever were.
func mustJSON(v map[string]any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// readAllStdin exists so tests can drive the password path without a terminal.
func defaultReadAllStdin() ([]byte, error) { return io.ReadAll(os.Stdin) }

var readAllStdin = defaultReadAllStdin

// errAuthNeedsUsername mirrors a rule the API enforces but only reports after
// the round trip, and its message names the field rather than the flag.
var errAuthNeedsUsername = clierr.BadInput(
	"--authentication-needed=true also needs --username")

// trunkDirectionOf reads a trunk's direction. Several write paths need it: the
// update API demands it on every call, and a number may only attach inbound.
func trunkDirectionOf(client *api.Client, trunkID string) (string, error) {
	var t api.SIPTrunk
	var apiErr *api.APIError
	err := readThrough(client, func() error {
		var e error
		apiErr, e = client.Do("GET", client.AccountURL("Zentrunk", "Trunk", trunkID), nil, nil, &t)
		return e
	})
	if err != nil {
		return "", err
	}
	if apiErr != nil {
		return "", apiErr
	}
	if t = unwrapSIPTrunk(t); t.TrunkDirection == "" {
		return "", clierr.BadInput("could not read the trunk's direction — pass --direction")
	}
	return t.TrunkDirection, nil
}

// readThrough runs a pre-flight GET even under --dry-run.
//
// client.Do short-circuits every request when DryRun is set, which silently
// disabled the guards built on top of a read: the preview then showed a POST
// that the real run would refuse. --dry-run means "send no writes", and a GET
// is not a write, so reads must still happen or the preview is a lie.
func readThrough(client *api.Client, fn func() error) error {
	was := client.DryRun
	client.DryRun = false
	defer func() { client.DryRun = was }()
	return fn()
}
