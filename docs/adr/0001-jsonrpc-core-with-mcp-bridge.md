# Agent Command は JSON-RPC 層に一本化し、MCP はブリッジとする

Agent Interface の Transport として MCP・JSON-RPC・CLI の三つを最初から提供する。Agent Command の実装はエミュレータ本体（GUI 版・headless 版の両方）に内蔵した JSON-RPC 2.0 サーバ（ローカルソケット、トークン認証）の一か所だけに置く。MCP は `shogun mcp` という stdio サーバとし、自分の中に headless の Instance を持つか、`--attach` で起動中の GUI へ JSON-RPC で接続して中継する。CLI（`shogun ctl`）は JSON-RPC クライアントとする。

GUI 版が MCP を直接話す案も検討した。しかしそれでは、人間が見ている GUI を AI と共有することと、MCP 以外のクライアント（スクリプト、CI）から使うことの両立が難しくなる。そのため JSON-RPC を共通の土台にした。
