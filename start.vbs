Set fso = CreateObject("Scripting.FileSystemObject")
Set sh = CreateObject("Wscript.Shell")
folder = fso.GetParentFolderName(WScript.ScriptFullName)
exe = folder & "\price-tracking-bot.exe"
If Not fso.FileExists(exe) Then
  sh.Popup "Сначала соберите бота: install-autostart.ps1", 8, "Трекинг цен", 48
  WScript.Quit 1
End If
sh.CurrentDirectory = folder
sh.Run Chr(34) & exe & Chr(34) & " -tray", 0, False
