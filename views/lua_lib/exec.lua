function execz(command, args)
    local result = exec(command, args)
    if result.status ~= 0 then
        error("Command returned non-0")
    end
    return result
end