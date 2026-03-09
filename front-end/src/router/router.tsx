import { createBrowserRouter } from "react-router"; // 注意检查这是 v6 还是 v7 的写法，App.tsx里用的是 react-router
import GlobalLayout from "../ui/layout/GlobalLayout";
import CanvasLayout from "../ui/layout/CanvasLayout";
import LandingPage from "../view";
import LoginPage from "../view/auth/Login";
import RegisterPage from "../view/auth/Register";
import ForgotPasswordPage from "../view/auth/ForgotPassword";
import Canvas from "../view/canvas";
import { authMiddleware } from "./middleware/auth";
import { canvasLayoutLoader, canvasLoader } from "./loader/canvas";
import NewCanvas from "../view/canvas/NewCanvas";
import MyResource from "../view/canvas/MyResource";
import SearchCanvases from "../view/canvas/SearchCanvases";

export const router = createBrowserRouter([
  {
    element: <GlobalLayout />,
    children: [
      {
        path: "/",
        element: <LandingPage />,
      },
      {
        path: "/login",
        element: <LoginPage />,
      },
      {
        path: "/register",
        element: <RegisterPage />,
      },
      {
        path: "/forgot-password",
        element: <ForgotPasswordPage />,
      },
      {
        path: "/about",
        element: <div>About</div>,
      },
      {
        path: "/canvas",
        element: <CanvasLayout />,
        loader: canvasLayoutLoader,
        middleware: [authMiddleware],
        children: [
          {
            index: true,
            element: <NewCanvas />,
          },
          {
            path: ":canvas_id",
            loader: canvasLoader,
            element: <Canvas />,
            shouldRevalidate: ({ currentUrl, nextUrl, defaultShouldRevalidate }) => {
              // 如果前后的 URL 路径完全一致，则返回 false（不重新运行 loader）
              if (currentUrl.pathname === nextUrl.pathname) {
                return false;
              }
              // 否则走默认的重新验证逻辑
              return defaultShouldRevalidate;
            },
          },
          {
            path: "search",
            element: <SearchCanvases />,
          },
          {
            path: "myresource",
            element: <MyResource />,
          }
        ],
      },
    ],
  },
]);