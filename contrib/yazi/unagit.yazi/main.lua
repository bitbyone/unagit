--- @since 26.9.1

return {
	entry = function(_, job)
		local args = { job.args[1] == "sessions" and "cd" or "go", "--print" }
		local permit = ui.hide()
		local child, err = Command("unagit"):arg(args)
			:stdin(Command.INHERIT):stdout(Command.PIPED):stderr(Command.INHERIT):spawn()
		local out
		if child then out, err = child:wait_with_output() end
		permit:drop()
		if not out then
			return ya.notify { title = "unagit", content = "cannot run the picker: " .. tostring(err), level = "error", timeout = 5 }
		end
		if not out.status.success then return end
		-- Only the printed newline is a separator; spaces belong to the name.
		local dir = out.stdout:gsub("\n$", "")
		if dir ~= "" then ya.emit("cd", { dir, raw = true }) end
	end,
}
