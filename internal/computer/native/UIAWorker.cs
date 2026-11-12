using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Runtime.InteropServices;
using System.Runtime.Serialization.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;
using System.Threading;
using System.Web.Script.Serialization;
using System.Windows;
using System.Windows.Automation;
using System.Xml;

// A single request runs on this windowless MTA thread. Process exit discards UIA objects,
// including when a provider blocks and the Go caller terminates this worker.
internal static class UIAWorker
{
    private const int MaxBytes = 256 * 1024;
    private const int MaxElements = 500;
    private const int MaxDepth = 12;
    private const string FixtureTitle = "Asimi Control Test Host";
    private const string Package = "Microsoft.WindowsNotepad_8wekyb3d8bbwe";
    private const string Stale = "CONTROL_STALE_TARGET";
    private const string Denied = "CONTROL_ACTION_DENIED";
    private const string Invalid = "CONTROL_INPUT_REJECTED";
    private const string Unknown = "CONTROL_UNKNOWN_RESULT";
    private static readonly Encoding Utf8 = new UTF8Encoding(false, true);
    private static readonly JavaScriptSerializer Json = new JavaScriptSerializer { MaxJsonLength = MaxBytes };
    private static readonly TreeWalker Walker = TreeWalker.RawViewWalker;
    private static readonly string[] ElementKeys = ("id name automationId controlType bounds enabled offscreen focused password " +
        "processId nativeHandle patterns parentPath runtimeId depth blank").Split(' ');
    private static readonly string[] IdentityKeys = ("name automationId controlType bounds enabled offscreen password blank " +
        "processId nativeHandle patterns parentPath runtimeId depth").Split(' ');
    private static readonly KeyValuePair<string, AutomationProperty>[] PatternProperties =
    {
        Flag("Invoke", AutomationElement.IsInvokePatternAvailableProperty),
        Flag("Value", AutomationElement.IsValuePatternAvailableProperty),
        Flag("Text", AutomationElement.IsTextPatternAvailableProperty),
        Flag("Toggle", AutomationElement.IsTogglePatternAvailableProperty),
        Flag("Selection", AutomationElement.IsSelectionPatternAvailableProperty),
        Flag("SelectionItem", AutomationElement.IsSelectionItemPatternAvailableProperty),
        Flag("Scroll", AutomationElement.IsScrollPatternAvailableProperty),
        Flag("ScrollItem", AutomationElement.IsScrollItemPatternAvailableProperty),
        Flag("Window", AutomationElement.IsWindowPatternAvailableProperty),
        Flag("ExpandCollapse", AutomationElement.IsExpandCollapsePatternAvailableProperty)
    };
    private static readonly Regex RestrictedText = new Regex(
        @"^\s*(?:powershell|pwsh|cmd|command|bash|sh|python|node|wscript|cscript|mshta|rundll32|regsvr32|curl|wget|git|sudo|ssh|runas|iex|invoke|start|set|net|reg)\b|\b(?:password|passwd|bearer|secret|token|api\s*key|credential)s?\b|[a-zA-Z0-9]{32}",
        RegexOptions.IgnoreCase | RegexOptions.CultureInvariant, TimeSpan.FromMilliseconds(100));
    private static bool issued;

    [MTAThread]
    private static void Main(string[] args)
    {
        using (var deadline = new Timer(delegate { Environment.Exit(124); }, null, 12000, Timeout.Infinite))
        {
            object response;
            try
            {
                Need(args.Length == 0, Invalid);
                response = Run(ParseRequest());
            }
            catch (Failure ex) { response = Error(ex.Code); }
            catch (XmlException) { response = Error(Invalid); }
            catch (DecoderFallbackException) { response = Error(Invalid); }
            catch (ElementNotAvailableException) { response = Error(Stale); }
            catch (Exception) { response = Error("CONTROL_UNAVAILABLE"); }

            byte[] bytes = Utf8.GetBytes(Json.Serialize(response));
            using (Stream output = Console.OpenStandardOutput())
                output.Write(bytes, 0, bytes.Length);
        }
    }

    private static object Run(Dictionary<string, object> request)
    {
        RequireKeys(request, "op window", "element text");
        string op = ReadString(request, "op", 16);
        Need(op == "inspect" || op == "invoke" || op == "set_value" || op == "type_text", Invalid);
        bool write = op == "set_value" || op == "type_text";
        Need(request.ContainsKey("element") == (op != "inspect") && request.ContainsKey("text") == write, Invalid);
        var window = ReadObject(request, "window");
        RequireKeys(window, "id app hwnd pid creationTime imagePath sessionId integrity className title bounds foreground observedAt",
            "imageHash packageFamily appUserModelId");
        ReadString(window, "id", 128);
        ReadString(window, "observedAt", 64);
        ReadBool(window, "foreground");
        ReadBounds(ReadObject(window, "bounds"));
        bool fixture = ReadString(window, "app", 16) == "test_host";
        Need(fixture || ReadString(window, "app", 16) == "notepad", Denied);
        Need(fixture || op == "inspect" || write, Denied);
        var hwnd = ReadHandle(window, "hwnd");
        uint pid = ReadUInt(window, "pid");
        ulong creation = ReadUInt64(window, "creationTime");
        Need(hwnd != IntPtr.Zero && pid != 0 && creation != 0, Invalid);

        Dictionary<string, object> expected = null;
        if (op != "inspect")
        {
            expected = ReadObject(request, "element");
            RequireKeys(expected, String.Join(" ", ElementKeys), "valueSha256 valueWritable");
            ValidateElement(expected);
        }
        string input = write ? ReadString(request, "text", 1024) : null;
        if (write) Need(AcceptedText(input), Invalid);

        Need(SetThreadDpiAwarenessContext(new IntPtr(-4)) != IntPtr.Zero, "CONTROL_UNAVAILABLE");
        using (var process = new SafeHandle(OpenProcess(0x1000, false, pid)))
        {
            Need(!process.IsInvalid, Stale);
            ValidateProcess(process, window, pid, creation, fixture);
            CheckWindow(hwnd, pid, window);
            var cache = ReadCache();
            var root = AutomationElement.FromHandle(hwnd).GetUpdatedCache(cache);
            var tree = new Tree { Modal = HasPopup(hwnd) };
            ReadTree(tree, root, cache, pid, fixture, 0, "");
            Need(tree.Nodes.Count > 0 && (string)tree.Nodes[0].Fields["nativeHandle"] == (string)window["hwnd"], Stale);
            CheckWindow(hwnd, pid, window);
            tree.Modal |= HasPopup(hwnd);
            if (op == "inspect")
                return new { elements = tree.Nodes.Select(node => node.Fields).ToArray(), truncated = tree.Truncated, modal = tree.Modal };

            Need(!tree.Truncated && !tree.Modal, Stale);
            string automationId = (string)expected["automationId"];
            string name = (string)expected["name"];
            string kind = (string)expected["controlType"];
            Node target;
            if (fixture)
            {
                Need(write
                    ? automationId == "TestInput" && kind == "Edit" && name == "Test input"
                    : kind == "Button" && ((automationId == "SubmitButton" && name == "Submit") ||
                                         (automationId == "ResetButton" && name == "Reset")), Denied);
                var matches = tree.Nodes.Where(node => (string)node.Fields["automationId"] == automationId).ToArray();
                Need(matches.Length == 1 && (write || tree.Nodes.Count(node =>
                    (string)node.Fields["controlType"] == "Button" && (string)node.Fields["name"] == name) == 1), Stale);
                target = matches[0];
            }
            else
            {
                // Count every editor, including hidden and disabled controls.
                // A search box or second tab editor makes the target ambiguous.
                Need(SingleNotepadEditor(tree.Nodes.Select(node => node.Fields)), Denied);
                target = tree.Nodes.Single(node => IsEditor(node.Fields));
                Need(SafeNotepadFields(expected), Denied);
            }
            var live = target.Element.GetUpdatedCache(cache);
            var fields = Describe(live, pid, fixture, (int)target.Fields["depth"], (string)target.Fields["parentPath"]);
            Need(SameElement(expected, fields) && (bool)fields["enabled"] && !(bool)fields["offscreen"] &&
                !(bool)fields["password"] && HasSameParents(root, live, (string)expected["parentPath"]), Stale);
            var bounds = ReadBounds((Dictionary<string, object>)fields["bounds"]);
            Need(Contained(bounds, ReadBounds(ReadObject(window, "bounds"))), Stale);
            object pattern;
            Need(live.TryGetCurrentPattern(write ? ValuePattern.Pattern : InvokePattern.Pattern, out pattern), Stale);
            if (write) Need(!((ValuePattern)pattern).Current.IsReadOnly, Denied);
            ValidateProcess(process, window, pid, creation, fixture);
            CheckWindow(hwnd, pid, window);
            Need(!HasPopup(hwnd) && live.Current.IsEnabled && !live.Current.IsOffscreen && !live.Current.IsPassword, Stale);
            if (!fixture)
            {
                var finalFields = Describe(live.GetUpdatedCache(cache), pid, false,
                    (int)target.Fields["depth"], (string)target.Fields["parentPath"]);
                Need(SameElement(expected, finalFields) && SafeNotepadFields(finalFields) &&
                    HasSameParents(root, live, (string)expected["parentPath"]), Stale);
                // ValuePattern is the sole mutation path. TextPattern-only
                // editors have no fallback, and existing text is never replaced.
                Need(!((ValuePattern)pattern).Current.IsReadOnly && ((ValuePattern)pattern).Current.Value.Length == 0, Stale);
            }
            issued = true;
            if (write) ((ValuePattern)pattern).SetValue(input);
            else ((InvokePattern)pattern).Invoke();
            return new { effect = "ISSUED" };
        }
    }

    private static CacheRequest ReadCache()
    {
        var cache = new CacheRequest { TreeScope = TreeScope.Element, TreeFilter = Automation.RawViewCondition };
        foreach (var property in new[]
        {
            AutomationElement.NameProperty, AutomationElement.AutomationIdProperty, AutomationElement.ControlTypeProperty,
            AutomationElement.BoundingRectangleProperty, AutomationElement.IsEnabledProperty, AutomationElement.IsOffscreenProperty,
            AutomationElement.HasKeyboardFocusProperty, AutomationElement.IsPasswordProperty, AutomationElement.ProcessIdProperty,
            AutomationElement.NativeWindowHandleProperty, AutomationElement.RuntimeIdProperty, WindowPattern.IsModalProperty
        })
            cache.Add(property);
        foreach (var flag in PatternProperties) cache.Add(flag.Value);
        return cache;
    }

    private sealed class Tree
    {
        internal readonly List<Node> Nodes = new List<Node>();
        internal int Bytes;
        internal int Visited;
        internal bool Truncated;
        internal bool Modal;
    }

    private sealed class Node
    {
        internal AutomationElement Element;
        internal Dictionary<string, object> Fields;
    }

    private static void ReadTree(Tree tree, AutomationElement element, CacheRequest cache, uint pid, bool fixture, int depth, string path)
    {
        if (++tree.Visited > MaxElements || depth > MaxDepth || path.Length > 1024)
        {
            tree.Truncated = true;
            return;
        }
        if (element.Cached.ProcessId != pid) return;
        var fields = Describe(element, pid, fixture, depth, path);
        if (fields == null)
        {
            tree.Truncated = true;
            return;
        }
        int bytes = Utf8.GetByteCount(Json.Serialize(fields)) + 1;
        if (tree.Bytes + bytes > MaxBytes - 512)
        {
            tree.Truncated = true;
            return;
        }
        tree.Bytes += bytes;
        tree.Nodes.Add(new Node { Element = element, Fields = fields });
        if (Object.Equals(element.GetCachedPropertyValue(WindowPattern.IsModalProperty, true), true) &&
            !element.Cached.IsOffscreen)
            tree.Modal = true;
        string kind = (string)fields["controlType"];
        // Exclude the editor subtree entirely below its metadata node.
        if (!fixture && (kind == "Edit" || kind == "Document")) return;
        string childPath = path + "/" + fields["runtimeId"];
        for (var child = Walker.GetFirstChild(element, cache); child != null; child = Walker.GetNextSibling(child, cache))
        {
            ReadTree(tree, child, cache, pid, fixture, depth + 1, childPath);
            if (tree.Truncated) break;
        }
    }

    private static Dictionary<string, object> Describe(AutomationElement element, uint pid, bool fixture, int depth, string path)
    {
        var p = element.Cached;
        if (p.ProcessId != pid) return null;
        string kind = p.ControlType.ProgrammaticName.Substring("ControlType.".Length);
        string name = !fixture && (kind == "Edit" || kind == "Document") ? "" : p.Name;
        string automationId = p.AutomationId;
        string runtimeId = RuntimeId((int[])element.GetCachedPropertyValue(AutomationElement.RuntimeIdProperty));
        if (runtimeId.Length == 0 || name.Length > 256 || automationId.Length > 256 || kind.Length > 64) return null;
        string[] patterns = PatternProperties
            .Where(flag => Object.Equals(element.GetCachedPropertyValue(flag.Value, true), true))
            .Select(flag => flag.Key).OrderBy(value => value, StringComparer.Ordinal).ToArray();
        var bounds = p.BoundingRectangle;
        var fields = new Dictionary<string, object>
        {
            { "id", "" }, { "name", name }, { "automationId", automationId }, { "controlType", kind },
            { "bounds", Bounds(bounds.IsEmpty ? new Rect() : bounds) },
            { "enabled", p.IsEnabled }, { "offscreen", p.IsOffscreen }, { "focused", p.HasKeyboardFocus },
            { "password", p.IsPassword }, { "processId", pid },
            { "nativeHandle", unchecked((uint)p.NativeWindowHandle).ToString(CultureInfo.InvariantCulture) },
            { "patterns", patterns }, { "parentPath", path }, { "runtimeId", runtimeId }, { "depth", depth }, { "blank", false }
        };
        if (!p.IsPassword && patterns.Contains("Value") &&
            (fixture ? automationId == "TestInput" && kind == "Edit" : kind == "Edit" || kind == "Document"))
        {
            object pattern;
            if (element.TryGetCurrentPattern(ValuePattern.Pattern, out pattern))
            {
                string value = ((ValuePattern)pattern).Current.Value;
                if (!fixture) fields["valueWritable"] = !((ValuePattern)pattern).Current.IsReadOnly;
                if (value.Length <= 1024)
                {
                    fields["blank"] = value.Length == 0;
                    using (var sha = SHA256.Create())
                        fields["valueSha256"] = BitConverter.ToString(sha.ComputeHash(Utf8.GetBytes(value))).Replace("-", "").ToLowerInvariant();
                }
            }
        }
        return fields;
    }

    private static bool IsEditor(Dictionary<string, object> fields)
    {
        return fields != null && ((string)fields["controlType"] == "Edit" || (string)fields["controlType"] == "Document");
    }

    private static bool SafeNotepadFields(Dictionary<string, object> fields)
    {
        return IsEditor(fields) && (string)fields["controlType"] == "Document" &&
            (bool)fields["enabled"] && !(bool)fields["offscreen"] && !(bool)fields["password"] &&
            (bool)fields["blank"] && fields.ContainsKey("valueWritable") && Object.Equals(fields["valueWritable"], true) &&
            fields.ContainsKey("valueSha256") && Object.Equals(fields["valueSha256"],
                "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") &&
            ((System.Collections.IEnumerable)fields["patterns"]).Cast<object>().Any(pattern => Object.Equals(pattern,"Value"));
    }

    private static bool SingleNotepadEditor(IEnumerable<Dictionary<string, object>> fields)
    {
        var editors = fields.Where(IsEditor).ToArray();
        return editors.Length == 1 && SafeNotepadFields(editors[0]);
    }

    private static bool SameElement(Dictionary<string, object> expected, Dictionary<string, object> current)
    {
        if (current == null) return false;
        return IdentityKeys.Where(key => key != "bounds" && key != "patterns" && key != "processId" && key != "depth")
            .All(key => Object.Equals(expected[key], current[key])) &&
            ReadUInt(expected, "processId") == (uint)current["processId"] && ReadUInt(expected, "depth") == (int)current["depth"] &&
            ReadBounds(ReadObject(expected, "bounds")).SequenceEqual(ReadBounds(ReadObject(current, "bounds"))) &&
            ((List<object>)expected["patterns"]).Cast<string>().OrderBy(value => value, StringComparer.Ordinal)
                .SequenceEqual((string[])current["patterns"]) &&
            expected.ContainsKey("valueSha256") == current.ContainsKey("valueSha256") &&
            (!expected.ContainsKey("valueSha256") || Object.Equals(expected["valueSha256"], current["valueSha256"])) &&
            expected.ContainsKey("valueWritable") == current.ContainsKey("valueWritable") &&
            (!expected.ContainsKey("valueWritable") || Object.Equals(expected["valueWritable"], current["valueWritable"]));
    }

    private static bool HasSameParents(AutomationElement root, AutomationElement element, string expected)
    {
        string[] path = expected.Split(new[] { '/' }, StringSplitOptions.RemoveEmptyEntries)
            .Concat(new[] { RuntimeId(element.GetRuntimeId()) }).ToArray();
        if (path.Length < 2 || path.Length > MaxDepth + 1 || path[0] != RuntimeId(root.GetRuntimeId())) return false;
        AutomationElement parent = root;
        int examined = 0;
        foreach (string runtime in path.Skip(1))
        {
            AutomationElement match = null;
            for (var child = Walker.GetFirstChild(parent); child != null; child = Walker.GetNextSibling(child))
            {
                if (++examined > MaxElements) return false;
                if (RuntimeId(child.GetRuntimeId()) != runtime) continue;
                if (match != null) return false;
                match = child;
            }
            if (match == null) return false;
            parent = match;
        }
        return parent.Equals(element);
    }

    private static void ValidateProcess(SafeHandle process, Dictionary<string, object> window, uint pid, ulong creation, bool fixture)
    {
        ulong started, exited, kernel, user;
        Need(GetProcessTimes(process, out started, out exited, out kernel, out user) && started == creation && exited == 0, Stale);
        var path = new StringBuilder(2048);
        int count = path.Capacity;
        Need(QueryFullProcessImageName(process, 0, path, ref count) &&
            String.Equals(path.ToString(), ReadString(window, "imagePath", 2048), StringComparison.OrdinalIgnoreCase), Stale);
        uint session, ownSession;
        Need(ProcessIdToSessionId(pid, out session) && ProcessIdToSessionId(GetCurrentProcessId(), out ownSession) &&
            session != 0 && session == ownSession && session == ReadUInt(window, "sessionId"), Stale);
        Need(ReadUInt(window, "integrity") == 0x2000 && IsMedium(process.DangerousGetHandle()) && IsMedium(GetCurrentProcess()), Denied);
        string className = ReadString(window, "className", 255);
        string title = ReadString(window, "title", 512);
        if (fixture)
        {
            var sibling = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "DeskPilotControlHost.exe");
            Need(String.Equals(path.ToString(), sibling, StringComparison.OrdinalIgnoreCase) &&
                ReadString(window, "imageHash", 64) == ControlHostBuild.Sha256 &&
                !window.ContainsKey("packageFamily") && !window.ContainsKey("appUserModelId") &&
                className.StartsWith("WindowsForms10.Window.8.app.", StringComparison.Ordinal) && title == FixtureTitle &&
                ParentOf(process.DangerousGetHandle()) != IntPtr.Zero &&
                ParentOf(process.DangerousGetHandle()) == ParentOf(GetCurrentProcess()), Denied);
        }
        else
        {
            string package = PackageName(process, GetPackageFullName);
            string expected = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles),
                "WindowsApps", package, "Notepad", "Notepad.exe");
            Need(className == "Notepad" && !window.ContainsKey("imageHash") &&
                ReadString(window, "packageFamily", 128) == Package && PackageName(process, GetPackageFamilyName) == Package &&
                ReadString(window, "appUserModelId", 128) == Package + "!App" &&
                PackageName(process, GetApplicationUserModelId) == Package + "!App" &&
                String.Equals(path.ToString(), expected, StringComparison.OrdinalIgnoreCase), Denied);
        }
    }

    private static void CheckWindow(IntPtr hwnd, uint pid, Dictionary<string, object> window)
    {
        uint actual;
        Need(GetWindowThreadProcessId(hwnd, out actual) != 0 && actual == pid && IsWindowVisible(hwnd) &&
            !IsIconic(hwnd) && GetAncestor(hwnd, 2) == hwnd && GetWindow(hwnd, 4) == IntPtr.Zero, Stale);
        var text = new StringBuilder(514);
        Need(GetClassName(hwnd, text, text.Capacity) > 0 && text.ToString() == (string)window["className"], Stale);
        GetWindowText(hwnd, text, text.Capacity);
        Need(text.ToString() == (string)window["title"], Stale);
        NativeRect rect;
        Need(GetWindowRect(hwnd, out rect) &&
            new[] { rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top }
                .SequenceEqual(ReadBounds(ReadObject(window, "bounds"))), Stale);
    }

    private static bool HasPopup(IntPtr hwnd)
    {
        return !IsWindowEnabled(hwnd) || GetLastActivePopup(hwnd) != hwnd;
    }

    private static bool Contained(int[] inner, int[] outer)
    {
        return inner[2] > 0 && inner[3] > 0 && inner[0] >= outer[0] && inner[1] >= outer[1] &&
            (long)inner[0] + inner[2] <= (long)outer[0] + outer[2] && (long)inner[1] + inner[3] <= (long)outer[1] + outer[3];
    }

    private static Dictionary<string, object> Bounds(Rect rect)
    {
        return new Dictionary<string, object>
        {
            { "x", checked((int)rect.X) }, { "y", checked((int)rect.Y) },
            { "width", checked((int)rect.Width) }, { "height", checked((int)rect.Height) }
        };
    }

    private static int[] ReadBounds(Dictionary<string, object> bounds)
    {
        RequireKeys(bounds, "x y width height", "");
        return new[] { "x", "y", "width", "height" }.Select(key =>
        {
            object value = bounds[key];
            Need(value is int || value is long && (long)value >= Int32.MinValue && (long)value <= Int32.MaxValue, Invalid);
            return Convert.ToInt32(value, CultureInfo.InvariantCulture);
        }).ToArray();
    }

    private static bool AcceptedText(string text)
    {
        if (String.IsNullOrWhiteSpace(text) || RestrictedText.IsMatch(text)) return false;
        int length = 0;
        for (int i = 0; i < text.Length; i++)
        {
            if (++length > 512 || Char.IsSurrogate(text[i]) && !Char.IsSurrogatePair(text, i)) return false;
            var category = CharUnicodeInfo.GetUnicodeCategory(text, i);
            if (category > UnicodeCategory.OtherNumber && text[i] != ' ' && ".,!?'，。！？、".IndexOf(text[i]) < 0)
                return false;
            if (Char.IsHighSurrogate(text[i])) i++;
        }
        return true;
    }

    private static void ValidateElement(Dictionary<string, object> element)
    {
        ReadString(element, "id", 128);
        ReadString(element, "name", 256);
        ReadString(element, "automationId", 256);
        ReadString(element, "controlType", 64);
        ReadString(element, "runtimeId", 256);
        ReadString(element, "parentPath", 1024);
        ReadHandle(element, "nativeHandle");
        ReadUInt(element, "processId");
        Need(ReadUInt(element, "depth") <= MaxDepth, Invalid);
        ReadBounds(ReadObject(element, "bounds"));
        foreach (string key in new[] { "enabled", "offscreen", "focused", "password", "blank" }) ReadBool(element, key);
        if (element.ContainsKey("valueSha256")) ReadString(element, "valueSha256", 64);
        if (element.ContainsKey("valueWritable")) ReadBool(element, "valueWritable");
        var patterns = element["patterns"] as List<object>;
        Need(patterns != null && patterns.Count <= PatternProperties.Length &&
            patterns.All(pattern => pattern is string && PatternProperties.Any(flag => flag.Key == (string)pattern)), Invalid);
    }

    private static Dictionary<string, object> ParseRequest()
    {
        using (Stream input = Console.OpenStandardInput())
        using (var buffer = new MemoryStream())
        {
            var chunk = new byte[4096];
            int read;
            while ((read = input.Read(chunk, 0, chunk.Length)) != 0)
            {
                Need(buffer.Length + read <= MaxBytes, Invalid);
                buffer.Write(chunk, 0, read);
            }
            byte[] bytes = buffer.ToArray();
            Utf8.GetCharCount(bytes); // Reject malformed UTF-8 before the JSON decoder.
            var quotas = new XmlDictionaryReaderQuotas { MaxDepth = 8, MaxStringContentLength = 4096 };
            using (var reader = JsonReaderWriterFactory.CreateJsonReader(bytes, quotas))
            {
                reader.MoveToContent();
                var request = ParseValue(reader) as Dictionary<string, object>;
                Need(request != null && reader.MoveToContent() == XmlNodeType.None, Invalid);
                return request;
            }
        }
    }

    private static object ParseValue(XmlReader reader)
    {
        string type = reader.GetAttribute("type");
        if (type == "string") return reader.ReadElementContentAsString();
        if (type == "boolean") return reader.ReadElementContentAsBoolean();
        if (type == "number") return reader.ReadElementContentAsLong();
        Need(type == "object" || type == "array", Invalid);
        var fields = new Dictionary<string, object>(StringComparer.Ordinal);
        var array = new List<object>();
        bool empty = reader.IsEmptyElement;
        reader.ReadStartElement();
        if (!empty)
        {
            while (reader.MoveToContent() == XmlNodeType.Element)
            {
                string key = reader.LocalName;
                Need(reader.NamespaceURI.Length == 0 && !fields.ContainsKey(key) && fields.Count + array.Count < 40, Invalid);
                object value = ParseValue(reader);
                if (type == "object") fields.Add(key, value);
                else array.Add(value);
            }
            reader.ReadEndElement();
        }
        if (type == "object") return fields;
        return array;
    }

    private static void RequireKeys(Dictionary<string, object> fields, string required, string optional)
    {
        string[] keys = required.Split(' ');
        Need(keys.All(fields.ContainsKey) && fields.Keys.All(key => keys.Contains(key) || optional.Split(' ').Contains(key)), Invalid);
    }

    private static Dictionary<string, object> ReadObject(Dictionary<string, object> fields, string key)
    {
        object value;
        Need(fields.TryGetValue(key, out value) && value is Dictionary<string, object>, Invalid);
        return (Dictionary<string, object>)value;
    }

    private static string ReadString(Dictionary<string, object> fields, string key, int max)
    {
        object value;
        Need(fields.TryGetValue(key, out value) && value is string && ((string)value).Length <= max, Invalid);
        return (string)value;
    }

    private static bool ReadBool(Dictionary<string, object> fields, string key)
    {
        object value;
        Need(fields.TryGetValue(key, out value) && value is bool, Invalid);
        return (bool)value;
    }

    private static uint ReadUInt(Dictionary<string, object> fields, string key)
    {
        object value = fields[key];
        Need(value is long && (long)value >= 0 && (long)value <= UInt32.MaxValue, Invalid);
        return (uint)(long)value;
    }

    private static ulong ReadUInt64(Dictionary<string, object> fields, string key)
    {
        ulong value;
        string raw = ReadString(fields, key, 20);
        Need(UInt64.TryParse(raw, NumberStyles.None, CultureInfo.InvariantCulture, out value) &&
            raw == value.ToString(CultureInfo.InvariantCulture), Invalid);
        return value;
    }

    private static IntPtr ReadHandle(Dictionary<string, object> fields, string key)
    {
        ulong handle = ReadUInt64(fields, key);
        Need(handle <= UInt32.MaxValue, Invalid);
        return new IntPtr((long)handle);
    }

    private static string RuntimeId(int[] id)
    {
        return id != null && id.Length <= 24 ? String.Join(".", id.Select(n => n.ToString(CultureInfo.InvariantCulture))) : "";
    }

    private static KeyValuePair<string, AutomationProperty> Flag(string name, AutomationProperty property)
    {
        return new KeyValuePair<string, AutomationProperty>(name, property);
    }

    private static object Error(string code)
    {
        return new { effect = issued ? "UNKNOWN" : "NOT_STARTED", code = issued ? Unknown : code };
    }

    private static void Need(bool condition, string code)
    {
        if (!condition) throw new Failure(code);
    }

    private sealed class Failure : Exception
    {
        internal readonly string Code;
        internal Failure(string code) { Code = code; }
    }

    private static bool IsMedium(IntPtr process)
    {
        IntPtr token;
        if (!OpenProcessToken(process, 8, out token)) return false;
        using (var handle = new SafeHandle(token))
        {
            const int capacity = 256;
            IntPtr buffer = Marshal.AllocHGlobal(capacity);
            try
            {
                int length;
                if (!GetTokenInformation(handle, 26, buffer, capacity, out length) || Marshal.ReadInt32(buffer) != 0 ||
                    !GetTokenInformation(handle, 25, buffer, capacity, out length)) return false;
                IntPtr sid = Marshal.ReadIntPtr(buffer);
                byte count = Marshal.ReadByte(GetSidSubAuthorityCount(sid));
                return count != 0 && Marshal.ReadInt32(GetSidSubAuthority(sid, (uint)count - 1)) == 0x2000;
            }
            finally { Marshal.FreeHGlobal(buffer); }
        }
    }

    private static IntPtr ParentOf(IntPtr process)
    {
        BasicProcessInfo result;
        int size = Marshal.SizeOf(typeof(BasicProcessInfo));
        return NtQueryInformationProcess(process, 0, out result, size, IntPtr.Zero) == 0 ? result.ParentPid : IntPtr.Zero;
    }

    private delegate int PackageQuery(SafeHandle process, ref uint length, StringBuilder output);

    private static string PackageName(SafeHandle process, PackageQuery query)
    {
        var buffer = new StringBuilder(256);
        uint length = (uint)buffer.Capacity;
        Need(query(process, ref length, buffer) == 0 && length > 1, Denied);
        return buffer.ToString();
    }

    private sealed class SafeHandle : Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid
    {
        internal SafeHandle(IntPtr value) : base(true) { SetHandle(value); }
        protected override bool ReleaseHandle() { return CloseHandle(handle); }
    }

    [StructLayout(LayoutKind.Sequential)] private struct NativeRect { internal int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] private struct BasicProcessInfo
    {
        internal IntPtr ExitStatus, Peb, Affinity, Priority, Pid, ParentPid;
    }
    [DllImport("user32.dll")] private static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr hwnd);
    [DllImport("user32.dll")] private static extern bool IsWindowEnabled(IntPtr hwnd);
    [DllImport("user32.dll")] private static extern bool IsIconic(IntPtr hwnd);
    [DllImport("user32.dll")] private static extern IntPtr GetAncestor(IntPtr hwnd, uint flags);
    [DllImport("user32.dll")] private static extern IntPtr GetWindow(IntPtr hwnd, uint command);
    [DllImport("user32.dll")] private static extern IntPtr GetLastActivePopup(IntPtr hwnd);
    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr hwnd, out NativeRect rect);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetClassName(IntPtr hwnd, StringBuilder text, int count);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetWindowText(IntPtr hwnd, StringBuilder text, int count);
    [DllImport("kernel32.dll")] private static extern IntPtr OpenProcess(uint access, bool inherit, uint pid);
    [DllImport("kernel32.dll")] private static extern bool CloseHandle(IntPtr handle);
    [DllImport("kernel32.dll")] private static extern bool GetProcessTimes(SafeHandle process, out ulong start, out ulong exit, out ulong kernel, out ulong user);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] private static extern bool QueryFullProcessImageName(SafeHandle process, int flags, StringBuilder name, ref int count);
    [DllImport("kernel32.dll")] private static extern uint GetCurrentProcessId();
    [DllImport("kernel32.dll")] private static extern IntPtr GetCurrentProcess();
    [DllImport("kernel32.dll")] private static extern bool ProcessIdToSessionId(uint pid, out uint session);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, ExactSpelling = true)] private static extern int GetPackageFamilyName(SafeHandle process, ref uint length, StringBuilder buffer);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, ExactSpelling = true)] private static extern int GetPackageFullName(SafeHandle process, ref uint length, StringBuilder buffer);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, ExactSpelling = true)] private static extern int GetApplicationUserModelId(SafeHandle process, ref uint length, StringBuilder buffer);
    [DllImport("advapi32.dll")] private static extern bool OpenProcessToken(IntPtr process, uint access, out IntPtr token);
    [DllImport("advapi32.dll")] private static extern bool GetTokenInformation(SafeHandle token, int kind, IntPtr buffer, int capacity, out int written);
    [DllImport("advapi32.dll")] private static extern IntPtr GetSidSubAuthorityCount(IntPtr sid);
    [DllImport("advapi32.dll")] private static extern IntPtr GetSidSubAuthority(IntPtr sid, uint index);
    [DllImport("ntdll.dll")] private static extern int NtQueryInformationProcess(IntPtr process, int kind, out BasicProcessInfo buffer, int length, IntPtr written);
}
