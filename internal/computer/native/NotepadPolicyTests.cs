// Deterministic worker policy checks only. This harness never starts or writes
// an external application and is not real Notepad acceptance evidence.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Reflection;
using System.Web.Script.Serialization;

internal static class NotepadPolicyTests
{
    private static readonly JavaScriptSerializer Json = new JavaScriptSerializer();
    private static readonly Type Worker = typeof(UIAWorker);
    private static int checks;
    private static Dictionary<string, object> Editor()
    {
        return Json.Deserialize<Dictionary<string, object>>(@"{""id"":""editor"",""name"":"""",""automationId"":""TextEditor"",""controlType"":""Document"",""enabled"":true,""offscreen"":false,""password"":false,""blank"":true,""valueWritable"":true,""processId"":7,""runtimeId"":""1.2.3"",""parentPath"":""1.2"",""patterns"":[""Text"",""Value""],""valueSha256"":""e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855""}");
    }
    private static bool Allowed(Dictionary<string, object> e)
    {
        return (bool)Worker.GetMethod("SafeNotepadFields", BindingFlags.NonPublic | BindingFlags.Static).Invoke(null,new object[]{e});
    }
    private static bool Single(params Dictionary<string, object>[] fields)
    {
        return (bool)Worker.GetMethod("SingleNotepadEditor", BindingFlags.NonPublic | BindingFlags.Static).Invoke(null,new object[]{fields});
    }
    private static void Check(bool value, string message)
    {
        if (!value) throw new Exception(message);
        checks++;
    }
    public static int Main()
    {
        try
        {
        Check(Allowed(Editor()), "Blank writable Value editor denied");
        Check(Single(Editor()), "Single editor denied");
        Check(!Single(), "Missing editor allowed");
        Check(!Single(Editor(),Editor()), "Ambiguous editor allowed");
        var hidden=Editor();hidden["offscreen"]=true;Check(!Single(Editor(),hidden), "Hidden second editor ignored");
        foreach (string key in new[]{"blank","enabled","valueWritable"}) { var e=Editor();e[key]=false;Check(!Allowed(e),key+" guard missing"); }
        foreach (string key in new[]{"offscreen","password"}) { var e=Editor();e[key]=true;Check(!Allowed(e),key+" guard missing"); }
        foreach (string key in new[]{"valueSha256","valueWritable"}) { var e=Editor();e.Remove(key);Check(!Allowed(e),"missing "+key+" allowed"); }
        var text=Editor();text["patterns"]=new List<object>{"Text"};Check(!Allowed(text),"Text-only editor allowed");
        var nonempty=Editor();nonempty["valueSha256"]="changed";Check(!Allowed(nonempty),"nonempty hash allowed");
        var search=Editor();search["controlType"]="Button";Check(!Allowed(search),"non-editor allowed");
        var find=Editor();find["controlType"]="Edit";find["automationId"]="FindText";Check(!Allowed(find),"search-only edit mistaken for document");
        Console.WriteLine("Worker Notepad policy: " + checks + " deterministic checks passed. No native application effects.");
        return 0;
        }
        catch (Exception ex) { Console.Error.WriteLine(ex.Message); return 1; }
    }
}
