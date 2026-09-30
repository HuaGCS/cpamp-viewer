import { AppLifecycle } from "@/app/AppLifecycle";
import { AppBackground } from "@/components/common/AppBackground";
import { ViewerApp } from "@/viewer/ViewerApp";

function App() {
  return (
    <>
      <AppLifecycle />
      <AppBackground />
      <div className="app-content">
        <ViewerApp />
      </div>
    </>
  );
}

export default App;
