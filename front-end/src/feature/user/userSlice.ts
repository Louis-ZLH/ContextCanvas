import { createSlice } from "@reduxjs/toolkit";

export type ThemeName = "saas" | "cyber" | "paper";

const getInitialTheme = (): ThemeName => {
  const stored = localStorage.getItem("theme");
  if (stored === "saas" || stored === "cyber" || stored === "paper") {
    return stored;
  }
  return "saas";
};

const userSlice = createSlice({
  name: "user",
  initialState: {
    userUID: null as string | null,
    language: "en",
    email: null as string | null,
    theme: getInitialTheme(),
    username: "guest",
  },
  reducers: {
    setUser: (state, action) => {
      state.userUID = action.payload.userUID;
      state.username = action.payload.username;
      state.language = action.payload.language;
      state.email = action.payload.email;
      state.theme = action.payload.theme;
      localStorage.setItem("theme", action.payload.theme);
    },
    changeTheme: (state, action: { payload: ThemeName }) => {
      state.theme = action.payload;
      localStorage.setItem("theme", action.payload);
      document.documentElement.setAttribute("data-theme", action.payload);
    },
    changeLanguage: (state, action) => {
      state.language = action.payload;
    },
  },
});

export const { setUser, changeTheme, changeLanguage } = userSlice.actions;

export default userSlice.reducer;
