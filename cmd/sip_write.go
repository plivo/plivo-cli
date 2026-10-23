package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
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

var trunkStatusValues = []string{"enabled", "disabled"}

// trunkFlagDirections pairs each trunk flag that names an object with the one
// direction that uses it: an inbound trunk sends calls to its URIs, and an
// outbound trunk authenticates with a credential, an IP list or both.
var trunkFlagDirections = [][2]string{
	{"uri", dirInbound}, {"fallback-uri", dirInbound}, {"credential", dirOutbound}, {"ip-acl", dirOutbound},
}

// checkTrunkFlags refuses a flag the trunk's direction does not use, before
// any write. An empty value names nothing: --fallback-uri "" clears the
// fallback.
func checkTrunkFlags(cmd *cobra.Command, dir string) error {
	for _, fd := range trunkFlagDirections {
		if fd[1] != dir && cmd.Flags().Lookup(fd[0]).Value.String() != "" {
			return clierr.BadFlag(fd[0], fmt.Sprintf("is for an %s trunk, and this one is %s", fd[1], dir))
		}
	}
	return nil
}

// ─── helpers shared by the write verbs ───────────────────────────────────────

// sipDeleteForce backs --force on the SIP deletes: delete even though something
// depends on the object. It only counts on top of --yes.
var sipDeleteForce bool

// trunkFilters are the trunk-list filters that find the trunks using an object,
// keyed by the API path segment of the object being deleted.
var trunkFilters = map[string][]string{
	"URI":                 {"primary_uri_uuid", "fallback_uri_uuid"},
	"Credential":          {"credential_uuid"},
	"IPAccessControlList": {"ipacl_uuid"},
}

// trunksUsing names every trunk that points at an object, and returns their
// IDs. It reads the trunk list through the filters, every page, under
// --dry-run too, and checks each match again here, so a filter the server
// ignored cannot add a trunk that does not use the object. An error means the
// check did not finish.
func trunksUsing(client *api.Client, segment, uuid string) ([]string, []string, error) {
	var used, ids []string
	seen := map[string]bool{}
	err := readThrough(client, func() error {
		for _, filter := range trunkFilters[segment] {
			q := url.Values{}
			q.Set("limit", strconv.Itoa(maxListLimit))
			q.Set(filter, uuid)
			var decodeErr error
			_, _, _, err := walkPages(client, client.AccountURL("Zentrunk", "Trunk"), q, "objects", 0,
				func(rows []json.RawMessage) bool {
					for _, row := range rows {
						var t api.SIPTrunk
						if decodeErr = json.Unmarshal(row, &t); decodeErr != nil {
							return false
						}
						if ref := trunksReferencing([]api.SIPTrunk{t}, uuid); len(ref) > 0 && !seen[t.TrunkID] {
							seen[t.TrunkID] = true
							used = append(used, ref...)
							ids = append(ids, t.TrunkID)
						}
					}
					return true
				})
			if err == nil && decodeErr != nil {
				err = clierr.Upstream("a trunk in the list could not be read: " + decodeErr.Error())
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return used, ids, err
}

// trunksReferencing names every trunk pointing at uuid, so the user sees what a
// delete would detach before it happens rather than after. The API reads an ID
// in any case, so this compares in any case too.
func trunksReferencing(trunks []api.SIPTrunk, uuid string) []string {
	var out []string
	for _, t := range trunks {
		switch {
		case strings.EqualFold(uuid, t.PrimaryURIUUID):
			out = append(out, t.TrunkID+" ("+t.Name+", primary URI)")
		case strings.EqualFold(uuid, t.FallbackURIUUID):
			out = append(out, t.TrunkID+" ("+t.Name+", fallback URI)")
		case strings.EqualFold(uuid, t.CredentialUUID):
			out = append(out, t.TrunkID+" ("+t.Name+", credential)")
		case strings.EqualFold(uuid, t.IPACLUUID):
			out = append(out, t.TrunkID+" ("+t.Name+", IP ACL)")
		}
	}
	return out
}

// canonUUID returns a SIP object's ID as the API writes it: trimmed and
// lower-case. ok is false when it is not a UUID.
func canonUUID(v string) (id string, ok bool) {
	id = strings.ToLower(strings.TrimSpace(v))
	return id, looksLikeUUID(id)
}

// sipObjectArg runs run with its argument, a call, URI, credential or IP
// access control list UUID, as canonUUID returns it, and refuses one that is
// not a UUID before any request: it goes into the request path. The in-use check before a delete used to compare
// the ID as typed with the API's lower-case IDs: an upper-case one matched no
// trunk, so --yes deleted an object in use and the API deleted its trunks.
func sipObjectArg(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, ok := canonUUID(args[0])
		if !ok {
			return clierr.BadInput(fmt.Sprintf("%s needs a UUID, got %q", cmd.CommandPath(), args[0]))
		}
		return run(cmd, []string{id})
	}
}

// canonUUIDFlags does the same for the trunk flags that name a URI,
// credential or IP access control list. An empty value is not an ID and is
// left as given.
func canonUUIDFlags(cmd *cobra.Command) error {
	for _, name := range []string{"uri", "fallback-uri", "credential", "ip-acl"} {
		f := cmd.Flags().Lookup(name)
		if v := f.Value.String(); v != "" {
			id, ok := canonUUID(v)
			if !ok {
				return clierr.BadFlag(name, fmt.Sprintf("needs a UUID, got %q", v))
			}
			_ = f.Value.Set(id)
		}
	}
	return nil
}

// canonTrunkID returns a trunk ID trimmed; ok is false unless it is digits,
// as the API writes one. The ID goes into the request path, where anything
// else can reach another endpoint: ../Number did.
func canonTrunkID(v string) (id string, ok bool) {
	id = strings.TrimSpace(v)
	return id, id != "" && strings.Trim(id, "0123456789") == ""
}

// notTrunkID says why v is not a trunk ID, with an example of one.
func notTrunkID(v string) string {
	return fmt.Sprintf("needs a trunk ID, which is digits only, such as 12345678901234567: got %q", v)
}

const trunkIDHint = "`plivo sip trunks list` shows your trunks' IDs."

// sipTrunkArg is sipObjectArg for a trunk ID argument (canonTrunkID).
func sipTrunkArg(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, ok := canonTrunkID(args[0])
		if !ok {
			e := clierr.BadInput(cmd.CommandPath() + " " + notTrunkID(args[0]))
			e.Hint = trunkIDHint
			return e
		}
		return run(cmd, []string{id})
	}
}

// firstNumberOnTrunk reads the account's numbers page by page, under --dry-run
// too, and returns the first one routed to the trunk: one is enough to refuse.
// "" means every page was read and none is. No filter finds numbers by trunk,
// so a large account is many pages, and progress goes to stderr. An error
// means the check did not finish.
func firstNumberOnTrunk(client *api.Client, trunkID string) (string, error) {
	// A number carries its trunk in `application` as a Zentrunk resource path,
	// not in a trunk_id field.
	marker := "/Zentrunk/Trunk/" + trunkID + "/"
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxListLimit))
	var found string
	var decodeErr error
	read, pages := 0, 0
	err := readThrough(client, func() error {
		_, _, _, err := walkPages(client, client.AccountURL("Number"), q, "objects", 0,
			func(rows []json.RawMessage) bool {
				for _, row := range rows {
					var n struct {
						Number      string `json:"number"`
						Application string `json:"application"`
					}
					if decodeErr = json.Unmarshal(row, &n); decodeErr != nil {
						return false
					}
					if strings.Contains(n.Application, marker) {
						found = n.Number
						return false
					}
				}
				read += len(rows)
				if pages++; pages%10 == 0 && !quietFlag {
					fmt.Fprintf(os.Stderr, "Checked %d numbers so far for routing to this trunk...\n", read)
				}
				return true
			})
		return err
	})
	if err == nil && decodeErr != nil {
		err = clierr.Upstream("a number in the list could not be read: " + decodeErr.Error())
	}
	if err == nil && found == "" && !quietFlag {
		fmt.Fprintf(os.Stderr, "None of the %d numbers on the account is routed to this trunk.\n", read)
	}
	return found, err
}

// confirmDelete lets a delete through only with --yes or --dry-run, and one
// that would break something only with --force on top: deleting a URI also
// deletes the trunks it is the primary URI of, and deleting a trunk stops
// calls to every number routed to it.
func confirmDelete(action string, dependents []string) error {
	if len(dependents) == 0 {
		return confirmDestructive(action)
	}
	if (yesFlag || dryRunFlag) && sipDeleteForce {
		return nil
	}
	e := clierr.DestructiveRefused(action)
	e.Message += ": it is in use"
	e.Hint = "Repoint or remove what depends on it first, or pass --yes --force to delete it anyway."
	e.Context = map[string]any{"dependents": dependents}
	return e
}

// checkFailed stops a delete whose dependents could not all be read: deleting
// blind is how an in-use URI takes its trunks with it. The read's own error
// comes through (code, exit code, retryable flag, request id, hint), so a
// rejected login still exits 2 and a rate limit 4; only the message and hint
// add what was refused.
func checkFailed(action string, err error) error {
	var read *clierr.Error
	if !errors.As(err, &read) {
		// No answer from the API, or none it could parse: a transport failure.
		read = clierr.NetworkError("the Plivo API", err)
	}
	e := *read
	e.Message = fmt.Sprintf("refusing to %s: could not check what depends on it: %s", action, read.Message)
	e.Hint = strings.TrimSpace(read.Hint + " Nothing was deleted.")
	return &e
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

// requireName refuses a blank --name on a URI, trunk or IP access control list
// create. The API answers "name cannot be empty" to those, though its docs call
// the name optional; cobra only checks that the flag was passed.
func requireName(name string) error {
	if strings.TrimSpace(name) == "" {
		return clierr.BadInput("--name is required: the API refuses an empty name")
	}
	return nil
}

// emitWrite renders the result of an update or delete. These used to print
// prose on stderr and nothing on stdout, so `-o json` produced an empty stream
// and exit 0 — indistinguishable from success with no data to a jq pipeline.
func emitWrite(human string, fields map[string]any) error {
	return emitResponse(human, mustJSON(fields))
}

// emitResponse is emitWrite for a write whose API response has a body: -o json
// prints that body as the API sent it.
func emitResponse(human string, raw json.RawMessage) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, raw)
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
	trunkCreatePlatform                     string
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
it: that domain is what you paste into your platform.

--platform livekit|elevenlabs|retell|vapi checks the trunk against Plivo's guide
for that platform: it warns when the authentication is not the guide's, names
the command that makes a missing URI, credential or IP list, and prints
"recommended: --secure" where the guide uses secure trunking. It never turns
secure trunking on: that is billed per minute.`,
	Example: `  plivo sip trunks create --name my-trunk --direction inbound --uri <uri_uuid>
  plivo sip trunks create --name out --direction outbound --credential <uuid>
  plivo sip trunks create --name agent-out --direction outbound --platform livekit --credential <uuid> --secure`,
	Args: cobra.NoArgs,
	RunE: runSIPTrunksCreate,
}

var sipTrunksUpdateCmd = &cobra.Command{
	Use:   "update <trunk_id>",
	Short: "Update a trunk",
	Long: `Update a trunk.

Boolean and state flags take an explicit value so they can be reversed:
--secure=false and --status disabled both turn something off. A flag you do not
pass is left untouched, the fallback URI included. --fallback-uri "" removes
the fallback URI.`,
	Example: `  plivo sip trunks update <id> --status disabled
  plivo sip trunks update <id> --secure=false
  plivo sip trunks update <id> --fallback-uri ""   # remove the fallback URI`,
	Args: cobra.ExactArgs(1),
	RunE: sipTrunkArg(runSIPTrunksUpdate),
}

var sipTrunksDeleteCmd = &cobra.Command{
	Use:   "delete <trunk_id>",
	Short: "Delete a trunk (requires --yes)",
	Long: `Delete a trunk.

Reads every number on the account first: deleting a trunk detaches every number
routed to it, and inbound calls to those numbers stop. If one is routed there,
the delete is refused unless --force is added to --yes. If the check cannot
read every page, the delete is refused.`,
	Args: cobra.ExactArgs(1),
	RunE: sipTrunkArg(runSIPTrunksDelete),
}

func runSIPTrunksCreate(cmd *cobra.Command, args []string) error {
	if err := requireName(trunkCreateName); err != nil {
		return err
	}
	p, err := sipPlatformFlag(trunkCreatePlatform)
	if err != nil {
		return err
	}
	if err := canonUUIDFlags(cmd); err != nil {
		return err
	}
	switch trunkCreateDirection {
	case dirInbound:
		if trunkCreateURI == "" {
			e := clierr.BadInput("an inbound trunk needs --uri (the origination URI calls arrive on)")
			if p != nil {
				e.Hint = p.inboundTrunkHint()
			}
			return e
		}
	case dirOutbound:
		if trunkCreateCredential == "" && trunkCreateIPACL == "" {
			e := clierr.BadInput("an outbound trunk needs --credential or --ip-acl to authenticate your platform")
			if p != nil {
				e.Hint = p.outboundTrunkHint()
			}
			return e
		}
	default:
		return clierr.BadInput("--direction must be inbound or outbound")
	}
	if err := checkTrunkFlags(cmd, trunkCreateDirection); err != nil {
		return err
	}
	if p != nil {
		p.checkTrunk(trunkCreateDirection, trunkCreateCredential, trunkCreateIPACL, cmd.Flags().Changed("secure"))
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
	if err := canonUUIDFlags(cmd); err != nil {
		return err
	}
	if err := validateEnum("status", &trunkUpdateStatus, trunkStatusValues...); err != nil {
		return err
	}
	// validateEnum reads "" as not given, but --status "" was given: an empty
	// status is not one to send.
	if cmd.Flags().Changed("status") && trunkUpdateStatus == "" {
		return badEnumValue("status", "", "empty value", trunkStatusValues)
	}
	if err := validateEnum("direction", &trunkUpdateDirection, directionValues...); err != nil {
		return err
	}
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
		if cmd.Flags().Changed("direction") {
			return clierr.BadInput("--direction alone sends no change; pass a flag that changes something " +
				"(--name, --status, --uri, --fallback-uri, --credential, --ip-acl or --secure)")
		}
		return clierr.BadInput("nothing to update — pass at least one flag")
	}
	// The API rejects any update without trunk_direction, including one that
	// does not touch it, and clears a fallback URI an update leaves out: a
	// rename used to drop it. Both come from the stored trunk unless given, and
	// --fallback-uri "" leaves the fallback out on purpose, which clears it.
	keepFallback := !cmd.Flags().Changed("fallback-uri")
	if !keepFallback && trunkUpdateFallbackURI == "" {
		delete(body, "fallback_uri_uuid")
	}
	dir := trunkUpdateDirection
	if dir == "" || keepFallback {
		stored, err := readTrunk(client, args[0])
		if err != nil {
			return err
		}
		if dir == "" {
			dir = stored.TrunkDirection
		}
		if keepFallback && dir == dirInbound && stored.FallbackURIUUID != "" {
			body["fallback_uri_uuid"] = stored.FallbackURIUUID
		}
	}
	if dir == "" {
		return clierr.BadInput("could not read the trunk's direction — pass --direction")
	}
	if err := checkTrunkFlags(cmd, dir); err != nil {
		return err
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
	action := "delete trunk " + id
	client, _, err := getClient()
	if err != nil {
		return err
	}
	routed, err := firstNumberOnTrunk(client, id)
	if err != nil {
		return checkFailed(action, err)
	}
	var dependents []string
	if routed != "" {
		fmt.Fprintf(os.Stderr, "Number %s is routed to this trunk; deleting the trunk detaches it and any other number routed there.\n", routed)
		dependents = []string{"number " + routed}
	}
	if err := confirmDelete(action, dependents); err != nil {
		return err
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
	uriCreatePlatform, uriCreateTransport          string
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

--uri accepts host, host:port, host;transport=udp|tcp|tls, sip:user@host or
sips:host. The API refuses a scheme, so sip: is dropped and sips: becomes
;transport=tls, on port 5061 unless one is given, or with --platform on the
guide's TLS port where it gives one; with another transport it is refused.
stderr names the URI sent. The host is a name or a public IPv4 address:
Plivo does not take IPv6, and can never reach a private, loopback,
link-local, carrier-grade NAT, multicast, benchmarking, reserved or 0.0.0.0
one. It is checked before sending: a space, a bad port, an unknown transport
or a malformed host is refused. A missing port is fine: the platform decides
the default. --transport adds ;transport= to --uri.

--platform livekit|elevenlabs|retell|vapi fills in what Plivo's guide for that
platform gives: host, port and transport, so you add only your own details,
such as a LiveKit project host or a regional host with --uri. Flags you pass
always win; a host or transport the guide does not list is used as given,
with a warning.`,
	Example: `  plivo sip uris create --name eleven --uri "sip.rtc.elevenlabs.io:5060;transport=tcp"
  plivo sip uris create --name eleven --platform elevenlabs
  plivo sip uris create --name agent --platform livekit --uri <project>.sip.livekit.cloud`,
	Args: cobra.NoArgs,
	RunE: runSIPURIsCreate,
}

var sipURIsListCmd = &cobra.Command{Use: "list", Short: "List origination URIs", Args: cobra.NoArgs, RunE: runSIPURIsList}
var sipURIsGetCmd = &cobra.Command{Use: "get <uri_uuid>", Short: "Get one origination URI", Args: cobra.ExactArgs(1), RunE: sipObjectArg(runSIPURIsGet)}

var sipURIsUpdateCmd = &cobra.Command{
	Use:   "update <uri_uuid>",
	Short: "Update an origination URI",
	Long: `--authentication-needed takes a value so it reverses: --authentication-needed=false turns it off.

--uri is checked before sending, and a sip: or sips: scheme rewritten, as on
create.`,
	Args: cobra.ExactArgs(1),
	RunE: sipObjectArg(runSIPURIsUpdate),
}

var sipURIsDeleteCmd = &cobra.Command{
	Use:   "delete <uri_uuid>",
	Short: "Delete an origination URI (requires --yes)",
	Long: `Names any trunk using it as a primary or fallback URI first, and refuses an
in-use URI unless --force is added to --yes: the API deletes a trunk whose
primary URI it is. After the delete each of those trunks is read back, and
the result says which were deleted, which kept and which could not be read
(-o json: trunks_deleted, trunks_kept, trunks_unchecked). If the check fails,
the delete is refused.`,
	Args: cobra.ExactArgs(1),
	RunE: sipObjectArg(runSIPURIsDelete),
}

// normalizeSIPURI keeps whatever shape the user gave. A bare host is legal, so
// nothing here adds a port or a scheme; it only trims stray whitespace.
func normalizeSIPURI(v string) string { return strings.TrimSpace(v) }

func runSIPURIsCreate(cmd *cobra.Command, args []string) error {
	if err := requireName(uriCreateName); err != nil {
		return err
	}
	p, err := sipPlatformFlag(uriCreatePlatform)
	if err != nil {
		return err
	}
	if err := checkTransportFlag(uriCreateTransport); err != nil {
		return err
	}
	uri, err := presetURI(p, uriCreateURI, uriCreateTransport)
	if err != nil {
		return err
	}
	if uri == "" {
		return clierr.BadInput("--uri is required (host, host:port, host;transport=…, or sip:user@host)")
	}
	if uri, err = originationURI(uri); err != nil {
		return err
	}
	if p != nil {
		p.begin()
	}
	if uriCreateAuthNeeded && uriCreateUsername == "" {
		return errAuthNeedsUsername
	}
	if err := uriAuthConflict(cmd, uriCreateAuthNeeded, uriCreateUsername, uriCreatePasswordStdin); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"name": uriCreateName, "uri": uri}
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
	if err := fetchList(client, client.AccountURL("Zentrunk", "URI"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
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
	var uri string
	if cmd.Flags().Changed("uri") {
		var err error
		if uri, err = originationURI(uriUpdateURI); err != nil {
			return err
		}
	}
	if err := uriAuthConflict(cmd, uriUpdateAuthNeeded, uriUpdateUsername, uriUpdatePasswordStdin); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = uriUpdateName
	}
	if cmd.Flags().Changed("uri") {
		body["uri"] = uri
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
	// The API refuses a username or password unless authentication_needed is
	// true in the same call, so rotating a password without restating it fails.
	// Those fields are only legal in that state, so imply it.
	if _, hasPw := body["password"]; hasPw || body["username"] != nil {
		if !cmd.Flags().Changed("authentication-needed") {
			body["authentication_needed"] = true
		}
		// With authentication_needed true the API also demands a username, so a
		// password-only rotation is impossible on the wire. Carry the stored one.
		if body["username"] == nil {
			stored, err := storedUsername(client, "URI", args[0])
			if err != nil {
				return err
			}
			body["username"] = stored
		}
	}
	boolFlagPatch(cmd, "authentication-needed", "authentication_needed", uriUpdateAuthNeeded, body)
	// The username sent, given or carried: a password brings the stored one.
	if user, _ := body["username"].(string); uriUpdateAuthNeeded && cmd.Flags().Changed("authentication-needed") && user == "" {
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
// every trunk pointing at the object, then refuse without --yes, or without
// --force when a trunk uses it.
func deleteSIPObject(cmd *cobra.Command, uuid, segment, action string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	// Always read the dependents. --yes skips the confirmation, never the check:
	// the whole point is to know what a delete detaches, and that matters most
	// when nobody is there to be asked.
	used, ids, err := trunksUsing(client, segment, uuid)
	if err != nil {
		return checkFailed(action+uuid, err)
	}
	reportDependents(used)
	if err := confirmDelete(action+uuid, used); err != nil {
		return err
	}
	if err := deleteSIP(client, "Zentrunk", segment, uuid); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	human := fmt.Sprintf("Deleted %s %s", segment, uuid)
	fields := map[string]any{"uuid": uuid, "resource": segment, "deleted": true}
	if segment == "URI" {
		human += trunksAfterURIDelete(client, uuid, ids, fields)
	}
	return emitWrite(human, fields)
}

// trunksAfterURIDelete reads back each trunk that used a deleted URI, as the
// API's effect is only known for a primary URI: that trunk goes too. A 404 is
// a trunk deleted with it; a trunk read back is kept, with whether its primary
// and its fallback still point at the URI; one that cannot be read, an error
// status or no answer, is unchecked. It adds them to fields and returns them
// as the tail of the human line.
func trunksAfterURIDelete(client *api.Client, uuid string, ids []string, fields map[string]any) string {
	deleted, kept := []string{}, []map[string]any{}
	var keptText, unchecked []string
	for _, id := range ids {
		var t api.SIPTrunk
		apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Trunk", id), nil, nil, &t)
		switch {
		case err == nil && apiErr == nil:
			t = unwrapSIPTrunk(t)
			primary, fallback := strings.EqualFold(t.PrimaryURIUUID, uuid), strings.EqualFold(t.FallbackURIUUID, uuid)
			kept = append(kept, map[string]any{"trunk_id": id,
				"primary_points_at_deleted_uri": primary, "fallback_points_at_deleted_uri": fallback})
			var still []string
			if primary {
				still = append(still, "primary")
			}
			if fallback {
				still = append(still, "fallback")
			}
			text := id
			if len(still) > 0 {
				text += " (still its " + strings.Join(still, " and ") + " URI)"
			}
			keptText = append(keptText, text)
		case err == nil && apiErr.StatusCode == http.StatusNotFound:
			deleted = append(deleted, id)
		default:
			unchecked = append(unchecked, id)
		}
	}
	fields["trunks_deleted"], fields["trunks_kept"] = deleted, kept
	var tail string
	if len(deleted) > 0 {
		tail += "; trunks deleted with it: " + strings.Join(deleted, ", ")
	}
	if len(keptText) > 0 {
		tail += "; trunks kept: " + strings.Join(keptText, ", ")
	}
	if len(unchecked) > 0 {
		fields["trunks_unchecked"] = unchecked
		tail += "; could not read back trunks " + strings.Join(unchecked, ", ")
	}
	return tail
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
	Example: `  printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name c1 --username sipuser1 --password-stdin`,
	Args:    cobra.NoArgs,
	RunE:    runSIPCredsCreate,
}

var sipCredsListCmd = &cobra.Command{Use: "list", Short: "List credentials", Args: cobra.NoArgs, RunE: runSIPCredsList}
var sipCredsGetCmd = &cobra.Command{Use: "get <credential_uuid>", Short: "Get one credential", Args: cobra.ExactArgs(1), RunE: sipObjectArg(runSIPCredsGet)}

var sipCredsUpdateCmd = &cobra.Command{
	Use:   "update <credential_uuid>",
	Short: "Update a credential",
	Long:  "As with create, a new password is read from stdin only.",
	Args:  cobra.ExactArgs(1),
	RunE:  sipObjectArg(runSIPCredsUpdate),
}

var sipCredsDeleteCmd = &cobra.Command{
	Use:   "delete <credential_uuid>",
	Short: "Delete a credential (requires --yes)",
	Long:  "Names any trunk using it first; an in-use one needs --force on top of --yes. If the check fails, the delete is refused.",
	Args:  cobra.ExactArgs(1),
	RunE:  sipObjectArg(runSIPCredsDelete),
}

// credentialPassword is the API's rule for a credential password: 5 to 20
// letters, digits and the symbols ~!@#$%^&*()_+, with at least one symbol.
// It answers anything else with a 400, after the round trip.
var credentialPassword = regexp.MustCompile(`^[A-Za-z0-9~!@#$%^&*()_+]{5,20}$`)

// checkCredentialPassword applies that rule. The error never repeats the
// password.
func checkCredentialPassword(pw string) error {
	if !credentialPassword.MatchString(pw) || !strings.ContainsAny(pw, "~!@#$%^&*()_+") {
		return clierr.BadInput("the password on stdin must be 5 to 20 letters, digits and ~!@#$%^&*()_+, " +
			"with at least one of those symbols")
	}
	return nil
}

// credentialUsername is the API's rule for a credential username: 5 to 20
// letters and digits. It answers anything else "Username length should be
// between 5 and 20, should contain only alphanumeric characters".
var credentialUsername = regexp.MustCompile(`^[A-Za-z0-9]{5,20}$`)

// checkCredentialUsername applies that rule to --username. A username is not
// a secret, so the error names it.
func checkCredentialUsername(u string) error {
	if !credentialUsername.MatchString(u) {
		return clierr.BadFlag("username", fmt.Sprintf("%q must be 5 to 20 letters and digits", u))
	}
	return nil
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
	if err := checkCredentialUsername(credCreateUsername); err != nil {
		return err
	}
	pw, err := readPasswordStdin()
	if err != nil {
		return err
	}
	if err := checkCredentialPassword(pw); err != nil {
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
	if err := fetchList(client, client.AccountURL("Zentrunk", "Credential"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
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
	if cmd.Flags().Changed("username") {
		if err := checkCredentialUsername(credUpdateUsername); err != nil {
			return err
		}
	}
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
		if err := checkCredentialPassword(pw); err != nil {
			return err
		}
		body["password"] = pw
		// The API rejects a password with no username, so rotating a password
		// alone is impossible on the wire. Carry the stored username across
		// rather than making the user restate something that is not changing.
		if _, ok := body["username"]; !ok {
			stored, err := storedUsername(client, "Credential", args[0])
			if err != nil {
				return err
			}
			body["username"] = stored
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
	aclCreateName     string
	aclCreateIPs      []string
	aclCreatePlatform string
	aclUpdateName     string
	aclUpdateIPs      []string
)

var sipACLCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an IP access control list",
	Long: `Create an IP access control list.

--ip is repeatable and takes one IPv4 address or CIDR range each (no IPv6). A
comma-separated list is refused rather than split, and so is a private,
loopback, link-local, carrier-grade NAT, multicast, benchmarking, reserved or
0.0.0.0 address, or a range inside one of those blocks: Plivo never sees a
call come from one. A range that allows the whole internet is reported but
not blocked: it is occasionally deliberate, and refusing it outright would
push people to the console instead.

--platform vapi fills --ip with the addresses Plivo's Vapi guide allows. The
other platforms authenticate with a credential, so they have none to fill.
--ip you pass always wins.`,
	Example: `  plivo sip ip-acl create --name platform --ip <platform_ip> --ip <platform_cidr>
  plivo sip ip-acl create --name vapi --platform vapi`,
	Args: cobra.NoArgs,
	RunE: runSIPACLCreate,
}

var sipACLUpdateCmd = &cobra.Command{
	Use:   "update <ipacl_uuid>",
	Short: "Update an IP access control list",
	Long:  "--ip replaces the whole list rather than appending, so pass every address you want kept.",
	Args:  cobra.ExactArgs(1),
	RunE:  sipObjectArg(runSIPACLUpdate),
}

var sipACLDeleteCmd = &cobra.Command{
	Use:   "delete <ipacl_uuid>",
	Short: "Delete an IP access control list (requires --yes)",
	Long:  "Names any trunk using it first; an in-use one needs --force on top of --yes. If the check fails, the delete is refused.",
	Args:  cobra.ExactArgs(1),
	RunE:  sipObjectArg(runSIPACLDelete),
}

// riskyCIDR reports a range that allows far more than a platform needs. It
// warns and does not block: the customer may genuinely want it, and a hard
// refusal just moves the work to the console where nothing warns at all.
func riskyCIDR(entry string) string {
	e := strings.TrimSpace(entry)
	if e == "0.0.0.0/0" {
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
	if prefix < 8 {
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
	if err := requireName(aclCreateName); err != nil {
		return err
	}
	p, err := sipPlatformFlag(aclCreatePlatform)
	if err != nil {
		return err
	}
	ips := aclCreateIPs
	if p != nil {
		if ips, err = p.presetIPs(ips); err != nil {
			return err
		}
	}
	if len(ips) == 0 {
		return clierr.BadInput("at least one --ip is required")
	}
	if err := checkACLEntries(ips); err != nil {
		return err
	}
	if p != nil {
		p.begin()
	}
	warnRiskyIPs(ips)
	client, _, err := getClient()
	if err != nil {
		return err
	}
	created, err := postSIP(client, map[string]any{
		"name": aclCreateName, "ip_addresses": ips,
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
	if cmd.Flags().Changed("ip") {
		if err := checkACLEntries(aclUpdateIPs); err != nil {
			return err
		}
	}
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
	cf.StringVar(&trunkCreateName, "name", "", "trunk name (required)")
	cf.StringVar(&trunkCreateDirection, "direction", "", "inbound|outbound (required)")
	cf.StringVar(&trunkCreateURI, "uri", "", "primary origination URI uuid (inbound)")
	cf.StringVar(&trunkCreateFallbackURI, "fallback-uri", "", "fallback origination URI uuid")
	cf.StringVar(&trunkCreateCredential, "credential", "", "credential uuid (outbound)")
	cf.StringVar(&trunkCreateIPACL, "ip-acl", "", "IP access control list uuid (outbound)")
	cf.BoolVar(&trunkCreateSecure, "secure", false, "enable TLS/SRTP")
	cf.StringVar(&trunkCreatePlatform, "platform", "", "check against a platform's guide: livekit|elevenlabs|retell|vapi")
	markRequiredByHand(sipTrunksCreateCmd, "direction")

	uf := sipTrunksUpdateCmd.Flags()
	uf.StringVar(&trunkUpdateName, "name", "", "trunk name")
	uf.StringVar(&trunkUpdateStatus, "status", "", oneOf(trunkStatusValues))
	uf.StringVar(&trunkUpdateURI, "uri", "", "primary origination URI uuid")
	uf.StringVar(&trunkUpdateFallbackURI, "fallback-uri", "", "fallback origination URI uuid (\"\" removes it)")
	uf.StringVar(&trunkUpdateCredential, "credential", "", "credential uuid")
	uf.StringVar(&trunkUpdateIPACL, "ip-acl", "", "IP access control list uuid")
	uf.BoolVar(&trunkUpdateSecure, "secure", false, "enable TLS/SRTP (takes a value: --secure=false)")
	uf.StringVar(&trunkUpdateDirection, "direction", "", "inbound|outbound (read from the trunk when omitted)")

	ucf := sipURIsCreateCmd.Flags()
	ucf.StringVar(&uriCreateName, "name", "", "URI name (required)")
	ucf.StringVar(&uriCreateURI, "uri", "", "host, host:port, host;transport=…, or sip:user@host")
	ucf.StringVar(&uriCreateUsername, "username", "", "username when authentication is needed")
	ucf.BoolVar(&uriCreatePasswordStdin, "password-stdin", false, "read the URI password from stdin")
	ucf.BoolVar(&uriCreateAuthNeeded, "authentication-needed", false, "require authentication")
	ucf.StringVar(&uriCreatePlatform, "platform", "", "fill in a platform's published values: livekit|elevenlabs|retell|vapi")
	ucf.StringVar(&uriCreateTransport, "transport", "", "udp|tcp|tls, added to --uri as ;transport=")
	registerListFlags(sipURIsListCmd, &uriListLimit, &uriListOffset)
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
	markRequiredByHand(sipCredsCreateCmd, "username", "password-stdin")
	registerListFlags(sipCredsListCmd, &credListLimit, &credListOffset)
	cuf := sipCredsUpdateCmd.Flags()
	cuf.StringVar(&credUpdateName, "name", "", "credential name")
	cuf.StringVar(&credUpdateUsername, "username", "", "SIP username")
	cuf.BoolVar(&credUpdatePasswordStdin, "password-stdin", false, "read the password from stdin (required: every update rewrites it)")
	markRequiredByHand(sipCredsUpdateCmd, "password-stdin")

	acf := sipACLCreateCmd.Flags()
	acf.StringVar(&aclCreateName, "name", "", "list name (required)")
	acf.StringArrayVar(&aclCreateIPs, "ip", nil, "IP or CIDR (repeatable)")
	acf.StringVar(&aclCreatePlatform, "platform", "", "fill --ip with a platform's published addresses: vapi")
	auf := sipACLUpdateCmd.Flags()
	auf.StringVar(&aclUpdateName, "name", "", "list name")
	auf.StringArrayVar(&aclUpdateIPs, "ip", nil, "IP or CIDR (repeatable; replaces the list)")

	for _, c := range []*cobra.Command{sipTrunksDeleteCmd, sipURIsDeleteCmd, sipCredsDeleteCmd, sipACLDeleteCmd} {
		c.Flags().BoolVar(&sipDeleteForce, "force", false, "delete even though something depends on it (with --yes)")
	}
	for _, c := range []*cobra.Command{sipTrunksCreateCmd, sipURIsCreateCmd, sipACLCreateCmd} {
		_ = c.MarkFlagRequired("name")
	}

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
	b, err := output.Marshal(v)
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

// uriAuthConflict refuses --authentication-needed=false with --username or
// --password-stdin, which only apply with authentication on: the request
// would turn it off and set them at once.
func uriAuthConflict(cmd *cobra.Command, authNeeded bool, username string, passwordStdin bool) error {
	if cmd.Flags().Changed("authentication-needed") && !authNeeded && (username != "" || passwordStdin) {
		return clierr.BadInput("--authentication-needed=false turns authentication off, " +
			"so --username and --password-stdin do not apply: pass one or the other")
	}
	return nil
}

// storedUsername reads a URI's or credential's username, under --dry-run too:
// the API refuses a password sent without one, and a preview that skipped the
// read would fail on a username it never asked for.
func storedUsername(client *api.Client, segment, id string) (string, error) {
	var cur struct {
		Username string `json:"username"`
	}
	if e := getForCheck(client, client.AccountURL("Zentrunk", segment, id), &cur); e != nil {
		return "", e
	}
	if cur.Username == "" {
		return "", clierr.BadInput("could not read the current username, which the API requires alongside a password — pass --username too")
	}
	return cur.Username, nil
}

// readTrunk reads a trunk, under --dry-run too. A trunk update needs what it
// does not change: the direction, which the API demands on every call, and
// the fallback URI, which the API clears when an update leaves it out.
func readTrunk(client *api.Client, trunkID string) (api.SIPTrunk, error) {
	var t api.SIPTrunk
	var apiErr *api.APIError
	err := readThrough(client, func() error {
		var e error
		apiErr, e = client.Do("GET", client.AccountURL("Zentrunk", "Trunk", trunkID), nil, nil, &t)
		return e
	})
	if err != nil {
		return t, err
	}
	if apiErr != nil {
		return t, apiErr
	}
	return unwrapSIPTrunk(t), nil
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
