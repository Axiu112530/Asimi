using System;
using System.Drawing;
using System.Threading;
using System.Windows.Forms;

internal static class ControlHost
{
    [STAThread]
    private static void Main()
    {
        Application.EnableVisualStyles();
        Application.SetCompatibleTextRenderingDefault(false);
        using (var form = new Form
        {
            Name = "TestWindow",
            Text = "Asimi Control Test Host",
            ClientSize = new Size(450, 315),
            StartPosition = FormStartPosition.CenterScreen
        })
        {
            var input = new TextBox
            {
                Name = "TestInput", AccessibleName = "Test input",
                Location = new Point(20, 35), Width = 410, MaxLength = 1024
            };
            var submit = new Button
            {
                Name = "SubmitButton", Text = "Submit",
                Location = new Point(20, 73), Width = 90
            };
            var reset = new Button
            {
                Name = "ResetButton", Text = "Reset",
                Location = new Point(118, 73), Width = 90
            };
            var result = new Label
            {
                Name = "ResultLabel", Text = "Idle",
                Location = new Point(75, 116), AutoSize = true
            };
            var check = new CheckBox
            {
                Name = "TestCheckbox", Text = "Test option",
                Location = new Point(20, 145), AutoSize = true
            };
            var scroll = new Panel
            {
                Name = "TestScrollableArea", AccessibleName = "Scrollable test area",
                Location = new Point(20, 180), Size = new Size(410, 115),
                AutoScroll = true, AutoScrollMinSize = new Size(0, 220), BorderStyle = BorderStyle.FixedSingle
            };
            scroll.Controls.Add(new Label { Text = "Start of test area", AutoSize = true, Location = new Point(10, 10) });
            scroll.Controls.Add(new Label { Text = "End of test area", AutoSize = true, Location = new Point(10, 190) });
            submit.Click += (sender, args) => result.Text = "Submitted";
            reset.Click += (sender, args) =>
            {
                input.Clear();
                check.Checked = false;
                result.Text = "Reset";
            };
            form.Controls.AddRange(new Control[]
            {
                new Label { Text = "Test input", AutoSize = true, Location = new Point(20, 15) },
                input, submit, reset,
                new Label { Text = "Status", AutoSize = true, Location = new Point(20, 116) },
                result, check, scroll
            });
            form.ActiveControl = input;
            form.Shown += (sender, args) =>
            {
                var lifetime = new Thread(() =>
                {
                    try
                    {
                        // Only pipe lifetime is accepted. Any input closes this test process.
                        using (var pipe = Console.OpenStandardInput()) pipe.ReadByte();
                    }
                    finally
                    {
                        try { form.BeginInvoke(new Action(form.Close)); }
                        catch (InvalidOperationException) { }
                    }
                }) { IsBackground = true };
                lifetime.Start();
            };
            Application.Run(form);
        }
    }
}
