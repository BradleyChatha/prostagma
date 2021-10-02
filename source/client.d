module client;

import vibe.d, std, lumars;

void clientMain()
{
    const lua  = environment["PROSTAGMA_SCRIPT"];

    debug setLogLevel(LogLevel.debug_);

    try
    {
        auto uuid = clientRegister();
        if(uuid == UUID.init)
        {
            logInfo("Could not finish connection, trying again in 5 seconds");
            sleep(5.seconds);
            clientMain();
            return;
        }

        auto conn = initConnection(uuid);

        while(conn.connected)
        {
            auto  data = conn.receiveText();
            const json = parseJson(data);
            switch(json["type"].get!string)
            {
                case "trigger":
                    try onTrigger(lua, conn);
                    catch(Exception ex)
                        logError("Error during trigger: %s", ex.msg);
                    break;

                default:
                    logError("Unknown trigger type: %s", json["type"].get!string);
                    break;
            }
        }
    }
    catch(Exception ex)
    {
        logError("Error while running loop: %s", ex.msg);
    }

    logInfo("Lost connection, trying again in 5 seconds.");
    sleep(5.seconds);
    clientMain();
}

UUID clientRegister()
{
    const meth = environment["PROSTAGMA_HTTP_METHOD"];
    const addr = environment["PROSTAGMA_HOST"];
    const port = environment["PROSTAGMA_PORT"].to!ushort;
    const secr = environment["PROSTAGMA_SECRET"];
    const name = environment["PROSTAGMA_NAME"];
    logInfo("Attempting to register to server %s:%s as client %s", addr, port, name);
    UUID uuid;
    requestHTTP(format("%s://%s:%s/register/%s", meth, addr, port, name), 
        (scope HTTPClientRequest req)
        {
            req.method = HTTPMethod.POST;
            req.headers.addField("Authorization", "Bearer "~secr);
        }, 
        (scope HTTPClientResponse res)
        {
            if(res.statusCode != HTTPStatus.ok)
            {
                logFatal("Server returned status code %s - %s", res.statusCode, res.readJson()["error"].get!string);
                return;
            }

            uuid = parseUUID(res.readJson["uuid"].get!string);
            logInfo("Registered, my UUID is %s", uuid);
        }
    );

    return uuid;
}

WebSocket initConnection(UUID uuid)
{
    const name = environment["PROSTAGMA_NAME"];
    const meth = environment["PROSTAGMA_HTTP_METHOD"];
    const addr = environment["PROSTAGMA_HOST"];
    const port = environment["PROSTAGMA_PORT"].to!ushort;

    logInfo("Attempting to connect as client %s", name);

    auto conn = connectWebSocket(URL(format("%s://%s:%s/connect", meth, addr, port)));

    Json json = Json.emptyObject;
    json["name"] = name;
    json["uuid"] = uuid.toString();
    conn.send(json.toString());

    logInfo("Request made, awaiting response from server");

    return conn;
}

private WebSocket g_clientSocket;
void onTrigger(string file, WebSocket socket)
{
    g_clientSocket = socket;
    auto state = setupState();
    state.doFile(file);
}

LuaState setupState()
{
    auto state = LuaState(null);
    state.doString(import("lua_lib/inspect.lua"));
    state.doString(import("lua_lib/exec.lua"));
    state.register!exec("exec");
    state.register!path_build("path_build");
    state.register!dir_push("dir_push");
    state.register!dir_pop("dir_pop");
    state.register!proxy_download("download_proxy");

    return state;
}

struct ExecInfo
{
    string output;
    int status;
}
ExecInfo exec(string command, string[] args)
{
    logInfo("Executing command: %s %s", command, args);
    const results = executeShell(escapeShellCommand(command) ~ escapeShellCommand([""]~args));
    logDebug("Status %s:\n%s", results.status, results.output);
    return ExecInfo(results.output, results.status);
}

string path_build(string[] path)
{
    logDebug("Building path from: %s", path);
    return buildNormalizedPath(path);
}

void dir_push(LuaState* lua, string dir)
{
    lua.doString("_dir_stack = _dir_stack or {}");
    auto table = lua.globalTable.get!LuaTable("_dir_stack");
    table.set(table.length+1, getcwd());
    chdir(dir);
    logDebug("Directory is now %s", getcwd());
}

void dir_pop(LuaState* lua)
{
    auto table = lua.globalTable.get!LuaTable("_dir_stack");
    chdir(table.get!string(table.length));
    logDebug("Directory is now %s", getcwd());
    lua.doString("table.remove(_dir_stack)");
}

void proxy_download(string url, string file)
{
    Json json = Json.emptyObject;
    json["type"] = "download";
    json["url"] = url;

    logInfo("Asking server to download %s for us. Placing it into file %s", url, file);
    g_clientSocket.send(json.toString());

    auto f = File(file, "wb");

    ubyte[] fSizeBytes = g_clientSocket.receiveBinary();
    enforce(fSizeBytes.length == 8, "Expected 8 bytes, not "~fSizeBytes.length.to!string);
    const fSize = bigEndianToNative!ulong(fSizeBytes[0..8]);
    logDebug("File size is %s bytes", fSize);

    ubyte[4096] buffer;
    while(f.tell < fSize)
    {
        logDebug("Waiting");
        g_clientSocket.receive((scope msg){
            const amount = msg.read(buffer, IOMode.once);
            logDebug("Got %s bytes. Currently at %s/%s", amount, f.tell, fSize);
            f.rawWrite(buffer[0..amount]);
        });
    }

    logInfo("Download finished");
}