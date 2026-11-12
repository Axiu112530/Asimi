// Disposable, unsaved UI fixture and scoped UIA probe. No product functionality.
using System;
using System.IO;
using System.Linq;
using System.Diagnostics;
using System.Collections.Generic;
using System.Threading;
using System.Windows.Forms;
using System.Windows.Automation;
using System.Web.Script.Serialization;
using System.Runtime.InteropServices;
public static class Fixture {
    static JavaScriptSerializer json = new JavaScriptSerializer();
    [STAThread] public static void Main(string[] args) {
        if (args[0] == "fixture") { RunFixture(args[1]); return; }
        Exception error = null;
        var worker = new Thread(() => { try { if(args[0]=="apps") ProbeApps(args[1],args[2]); else Probe(args[1], args[2]); } catch(Exception e) { error = e; } });
        worker.SetApartmentState(ApartmentState.MTA); worker.Start();
        if (!worker.Join(args[0]=="apps"?30000:12000)) Environment.Exit(3);
        if (error != null) { Console.Error.WriteLine(error.GetType().Name + ": " + error.Message); Environment.Exit(2); }
    }
    delegate bool EnumProc(IntPtr h,IntPtr p);
    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc callback,IntPtr p);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
    [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,System.Text.StringBuilder text,int max);
    [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr h,int cmd);
    [DllImport("user32.dll")] static extern bool PostMessage(IntPtr h,uint msg,IntPtr w,IntPtr l);
    static List<IntPtr> WindowsFor(string name) {
        var ids=new HashSet<int>(Process.GetProcessesByName(name).Select(p=>p.Id));
        var result=new List<IntPtr>(); EnumWindows((h,p)=>{uint id;GetWindowThreadProcessId(h,out id);if(ids.Contains((int)id))result.Add(h);return true;},IntPtr.Zero);return result;
    }
    static void ProbeApps(string workspace,string output) {
        var results=new List<object>();
        string textFile=Path.Combine(workspace,"validation","phase0","bin","asimi-uia-fixture.txt");
        File.WriteAllText(textFile,"Asimi Phase 0 UIA fixture. No user content.");
        foreach(string name in new[]{"notepad","explorer","WindowsTerminal"}) {
            var baseline=new HashSet<IntPtr>(WindowsFor(name));
            string marker=name=="notepad"?"asimi-uia-fixture":name=="explorer"?"desktop-AI":"AsimiPhase0";
            string exe=name=="notepad"?"notepad.exe":name=="explorer"?"explorer.exe":"wt.exe";
            string arguments=name=="notepad"?'"'+textFile+'"':name=="explorer"?"/n,\""+workspace+"\"":"-w new new-tab --title AsimiPhase0 powershell.exe -NoProfile -NoLogo -Command \"Start-Sleep -Seconds 18\"";
            List<IntPtr> owned=new List<IntPtr>();
            try {
                var info=new ProcessStartInfo(exe,arguments){UseShellExecute=false,WindowStyle=ProcessWindowStyle.Hidden};
                using(var launched=Process.Start(info)){}
                for(int wait=0;wait<30;wait++) {
                    owned=WindowsFor(name).Where(h=>{if(baseline.Contains(h))return false;var b=new System.Text.StringBuilder(512);GetWindowText(h,b,512);return b.ToString().IndexOf(marker,StringComparison.OrdinalIgnoreCase)>=0;}).ToList();
                    if(owned.Count>0)break;Thread.Sleep(150);
                }
                int controls=0,buttons=0,edits=0,texts=0,invokes=0,values=0,selections=0;bool bounded=false;
                foreach(var h in owned){ShowWindow(h,5);Thread.Sleep(300);var root=AutomationElement.FromHandle(h);
                    var queue=new Queue<Tuple<AutomationElement,int>>();queue.Enqueue(Tuple.Create(root,0));
                    while(queue.Count>0&&controls<150){var entry=queue.Dequeue();var element=entry.Item1;controls++;var type=element.Current.ControlType;object pattern;
                        if(type==ControlType.Button)buttons++;if(type==ControlType.Edit)edits++;if(type==ControlType.Text)texts++;
                        if(element.TryGetCurrentPattern(InvokePattern.Pattern,out pattern))invokes++;if(element.TryGetCurrentPattern(ValuePattern.Pattern,out pattern))values++;if(element.TryGetCurrentPattern(SelectionPattern.Pattern,out pattern))selections++;
                        if(entry.Item2<4){var child=TreeWalker.ControlViewWalker.GetFirstChild(element);while(child!=null&&queue.Count<150){queue.Enqueue(Tuple.Create(child,entry.Item2+1));child=TreeWalker.ControlViewWalker.GetNextSibling(child);}}
                    }bounded=queue.Count>0;
                }
                results.Add(new {app=name,controlledWindows=owned.Count,controls,buttons,edits,texts,invokePatterns=invokes,valuePatterns=values,selectionPatterns=selections,truncated=bounded,mode="read-only-pattern-discovery"});
            }catch(Exception e){results.Add(new {app=name,error=e.GetType().Name});}
            finally{foreach(var h in owned)PostMessage(h,0x0010,IntPtr.Zero,IntPtr.Zero);}
        }
        results.Add(new {app="Code",standardUserInstall=File.Exists(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","Microsoft VS Code","Code.exe")),standardMachineInstall=File.Exists("C:\\Program Files\\Microsoft VS Code\\Code.exe"),tested=false});
        File.WriteAllText(output,json.Serialize(new {apartment="MTA",maxControls=150,maxDepth=4,applications=results}));
    }
    static void RunFixture(string path) {
        Application.EnableVisualStyles();
        var form = new Form { Text="Asimi Phase0 Fixture", Width=450, Height=280, StartPosition=FormStartPosition.CenterScreen };
        var edit = new TextBox { Name="probeEdit", AccessibleName="probeEdit", Left=20, Top=20, Width=380 };
        var label = new Label { Name="probeStatus", Text="idle", Left=20, Top=110, Width=380 };
        var button = new Button { Name="probeButton", AccessibleName="probeButton", Text="Probe", Left=20, Top=65, Width=100 };
        var list = new ListBox { Name="probeList", Left=160, Top=65, Width=200, Height=40 };
        list.Items.AddRange(new object[]{"alpha","beta"});
        button.Click += (s,e) => label.Text="clicked";
        edit.TextChanged += (s,e) => label.Text="edited";
        form.Controls.AddRange(new Control[]{edit,button,label,list});
        var timer = new System.Windows.Forms.Timer { Interval=55000 };
        timer.Tick += (s,e) => form.Close(); timer.Start();
        form.Shown += (s,e) => { form.Activate(); edit.Focus(); File.WriteAllText(path,json.Serialize(new {pid=Process.GetCurrentProcess().Id,window=form.Handle.ToInt64(),edit=edit.Handle.ToInt64(),button=button.Handle.ToInt64(),status=label.Handle.ToInt64()})); };
        Application.Run(form); timer.Dispose(); form.Dispose();
    }
    static void Probe(string input, string output) {
        var d = json.Deserialize<Dictionary<string,object>>(File.ReadAllText(input));
        var root=AutomationElement.FromHandle(new IntPtr(Convert.ToInt64(d["window"])));
        var edit=root.FindFirst(TreeScope.Descendants,new PropertyCondition(AutomationElement.AutomationIdProperty,"probeEdit"));
        var button=root.FindFirst(TreeScope.Descendants,new PropertyCondition(AutomationElement.AutomationIdProperty,"probeButton"));
        var list=root.FindFirst(TreeScope.Descendants,new PropertyCondition(AutomationElement.AutomationIdProperty,"probeList"));
        var value=(ValuePattern)edit.GetCurrentPattern(ValuePattern.Pattern); value.SetValue("uia-value");
        var valueOK=value.Current.Value=="uia-value";
        ((InvokePattern)button.GetCurrentPattern(InvokePattern.Pattern)).Invoke();
        var status=root.FindFirst(TreeScope.Descendants,new PropertyCondition(AutomationElement.AutomationIdProperty,"probeStatus"));
        for(int i=0;i<20 && status.Current.Name!="clicked";i++) Thread.Sleep(25);
        bool invokeVerified=status.Current.Name=="clicked";
        var items=list.FindAll(TreeScope.Children,new PropertyCondition(AutomationElement.ControlTypeProperty,ControlType.ListItem));
        ((SelectionItemPattern)items[1].GetCurrentPattern(SelectionItemPattern.Pattern)).Select();
        var selected=((SelectionPattern)list.GetCurrentPattern(SelectionPattern.Pattern)).Current.GetSelection();
        object textPattern; bool textSupported=edit.TryGetCurrentPattern(TextPattern.Pattern,out textPattern);
        bool textRead=textSupported && ((TextPattern)textPattern).DocumentRange.GetText(-1).Contains("uia-value");
        var apps=new List<object>();
        foreach (string name in new[]{"notepad","explorer","WindowsTerminal","Code"}) {
            var processes=Process.GetProcessesByName(name); int windows=0, controls=0, buttons=0, edits=0, texts=0;
            foreach (var p in processes) { using(p) { if(p.MainWindowHandle==IntPtr.Zero) continue; windows++;
                var el=AutomationElement.FromHandle(p.MainWindowHandle);
                // Bound the traversal to immediate children; never serialize names/text/paths.
                var children=el.FindAll(TreeScope.Children,Condition.TrueCondition); controls+=children.Count;
                foreach(AutomationElement child in children) { var t=child.Current.ControlType;
                    if(t==ControlType.Button)buttons++; if(t==ControlType.Edit)edits++; if(t==ControlType.Text)texts++; }
            } }
            apps.Add(new {app=name,processCount=processes.Length,topLevelWindows=windows,immediateChildren=controls,buttons,edits,texts});
        }
        File.WriteAllText(output,json.Serialize(new {apartment=Thread.CurrentThread.GetApartmentState().ToString(),fixture=new {valueRoundTrip=valueOK,invokeVerified,selection=selected.Length==1 && selected[0].Current.Name=="beta",textPattern=textSupported,textRead,buttonType=button.Current.ControlType.ProgrammaticName,descendants=root.FindAll(TreeScope.Descendants,Condition.TrueCondition).Count},apps}));
    }
}
