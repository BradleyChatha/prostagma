module server;

import std, vibe.d;

struct ClientInfo
{
    UUID uuid;
    bool shouldTrigger;
}

string g_secret;
ClientInfo[string] g_clients;

void serverMain()
{
    logInfo("Starting server");
    const addr = environment["PROSTAGMA_HOST"];
    const port = environment["PROSTAGMA_PORT"].to!ushort;
    g_secret   = environment["PROSTAGMA_SECRET"];

    auto settings = new HTTPServerSettings();
    settings.bindAddresses = [addr];
    settings.port = port;

    auto router = new URLRouter();
    router.post("/webhook/:name", &webhook);
    router.post("/register/:name", &register);
    router.get("/connect", handleWebSockets(&onConnect));
    
    auto l = listenHTTP(settings, router);

    string[] why;
    runApplication(&why);
    l.stopListening();
}

void onConnect(scope WebSocket socket) nothrow
{
    string name;
    try
    {
        auto chars = socket.receiveText();
        Json json = parseJson(chars);

        name = json["name"].get!string;
        scope ptr = name in g_clients;
        enforce(ptr !is null, "Client does not exist: "~name);
        enforce(ptr.uuid.toString() == json["uuid"].get!string, "Client UUID mismatch");

        logInfo("Accepted client %s", name);
    }
    catch(Exception ex)
    {
        try socket.close(WebSocketCloseReason.policyViolation, ex.msg);
        catch(Exception ex){}
        logError("Could not accept client: %s", ex.msg);
        return;
    }

    try
    {
        while(socket.connected)
        {
            sleep(1.seconds);

            if(socket.dataAvailableForRead)
            {
                auto data = socket.receiveText();
                Json json = parseJson(data);
                
                switch(json["type"].get!string)
                {
                    case "download":
                        logInfo("Client wants us to download: %s", json["url"]);

                        auto filename = randomUUID().toString();
                        vibe.inet.urltransfer.download(json["url"].get!string, filename);
                        scope(exit) std.file.remove(filename);
                        logInfo("File downloaded into: %s", filename);

                        auto file = File(filename, "rb");
                        file.seek(0, SEEK_END);
                        const length = file.tell;
                        file.seek(0);

                        logInfo("Sending file size");
                        socket.send(nativeToBigEndian(length));
                        
                        ubyte[4096] buffer;
                        while(file.tell < length)
                        {
                            const slice = file.rawRead(buffer);
                            logInfo("Sending next %s bytes. %s/%s", slice.length, file.tell, length);
                            socket.send(slice);
                        }
                        break;

                    default:
                        logError("Client %s send a bad message", name);
                        break;
                }
            }

            if(!g_clients[name].shouldTrigger)
                continue;

            Json json = Json.emptyObject;
            json["type"] = "trigger";
            socket.send(json.toString());
            logInfo("Sending trigger to client %s", name);
            g_clients[name].shouldTrigger = false;
        }
        logInfo("Client %s disconnected", name);
    }
    catch(Exception ex)
    {
        logError("Error while handling client %s: %s", name, ex.msg);
        try socket.close(WebSocketCloseReason.abnormalClosure, ex.msg);
        catch(Exception ex){}
    }

    logInfo("Client %s can now register again", name);
    g_clients.remove(name);
}

void webhook(HTTPServerRequest req, HTTPServerResponse res)
{
    const name = req.params["name"];
    scope ptr = name in g_clients;
    if(!ptr)
    {
        static struct Error { string error; }
        res.writeJsonBody(Error("Client does not exist."), HTTPStatus.forbidden);
        return;
    }

    ptr.shouldTrigger = true;
    res.writeJsonBody("ok", HTTPStatus.ok);
    logInfo("Client %s triggered by %s", name, req.peer);
}

void register(HTTPServerRequest req, HTTPServerResponse res)
{
    const name   = req.params["name"];
    const secret = getBearer(req.headers.get("Authorization"));

    logInfo("Client %s is attempting to register", name);

    if(secret != g_secret)
    {
        static struct Error{ string error; }
        res.writeJsonBody(Error("Invalid secret in `Authorization: Bearer` scheme."), HTTPStatus.unauthorized);
        logError("Rejected client %s due to an invalid secret", name);
        return;
    }

    if(ClientInfo* client = name in g_clients)
    {
        static struct Error { string error; }
        res.writeJsonBody(Error("Client with name '"~name~"' already exists."), HTTPStatus.conflict);
        logError("Rejected client %s due to the client already existing", name);
        return;
    }

    ClientInfo info;
    info.uuid = randomUUID();
    g_clients[name] = info;

    static struct Response { string uuid; }
    res.writeJsonBody(Response(info.uuid.toString()), HTTPStatus.ok);
    logInfo("Registered client %s", name);
}

string getBearer(string auth)
{
    static reg = regex("Bearer (.+)");
    const match = matchFirst(auth, reg);
    if(match.empty)
        return null;
    return match[1];
}