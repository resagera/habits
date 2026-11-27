package ru.resager.habitstv;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * Плеер медиатеки на весь экран.
 *
 * Зачем своё приложение, а не браузер приставки: встроенный браузер изображает
 * мышь, и по плиткам приходится водить курсором. WebView внутри приложения
 * получает настоящие кнопки пульта — стрелки и OK приходят в страницу как
 * ArrowUp…/Enter, а навигация по плиткам в ней уже есть.
 *
 * Страница узнаёт приложение по «HabitsTV/» в User-Agent: сама нажимает
 * «Начать» (автовоспроизведение здесь разрешено) и показывает кнопку смены адреса.
 */
public class MainActivity extends Activity {
    static final String PREFS = "habits_tv";
    static final String KEY_URL = "url";
    static final String DEFAULT_URL = "http://192.168.0.79/tv/";
    static final String VERSION = "1.0";

    private WebView web;
    private FrameLayout root;
    private LinearLayout errorBox;
    private TextView errorText;
    private View fullscreenView;
    private WebChromeClient.CustomViewCallback fullscreenCallback;
    private String loadedUrl = "";
    private boolean backLongPress;

    static String savedUrl(Context c) {
        return c.getSharedPreferences(PREFS, MODE_PRIVATE).getString(KEY_URL, DEFAULT_URL);
    }

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        // телевизор не должен гаснуть посреди серии
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        root = new FrameLayout(this);
        root.setBackgroundColor(Color.rgb(13, 15, 20));

        web = new WebView(this);
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        // без жеста на самой странице: иначе нужна кнопка «Начать»
        s.setMediaPlaybackRequiresUserGesture(false);
        s.setUserAgentString(s.getUserAgentString() + " HabitsTV/" + VERSION);
        web.setBackgroundColor(Color.rgb(13, 15, 20));
        web.setWebViewClient(new PageClient());
        web.setWebChromeClient(new FullscreenClient());
        web.addJavascriptInterface(new Bridge(), "HabitsApp");
        root.addView(web, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        root.addView(buildErrorBox(), new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        setContentView(root);
        load();
    }

    private void load() {
        errorBox.setVisibility(View.GONE);
        web.setVisibility(View.VISIBLE);
        loadedUrl = savedUrl(this);
        web.loadUrl(loadedUrl);
        web.requestFocus();
    }

    @Override
    protected void onResume() {
        super.onResume();
        web.onResume();
        hideSystemUi();
        // вернулись из настроек с новым адресом — открыть его
        if (!savedUrl(this).equals(loadedUrl)) {
            load();
        }
    }

    @Override
    protected void onPause() {
        // нажали «Домой» — видео не должно играть за кадром
        web.onPause();
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        web.destroy();
        super.onDestroy();
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            hideSystemUi();
        }
    }

    private void hideSystemUi() {
        getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY);
    }

    /**
     * «Назад» на пульте: коротко — шаг назад по странице (или выход с главной),
     * долго — настройка адреса. Долгое нажатие — единственный путь в настройки,
     * который работает, даже когда страница не открылась совсем.
     */
    @Override
    public boolean dispatchKeyEvent(KeyEvent e) {
        int code = e.getKeyCode();
        if (code == KeyEvent.KEYCODE_MENU && e.getAction() == KeyEvent.ACTION_UP) {
            openSettings();
            return true;
        }
        if (code != KeyEvent.KEYCODE_BACK) {
            return super.dispatchKeyEvent(e);
        }
        if (e.getAction() == KeyEvent.ACTION_DOWN) {
            if (e.getRepeatCount() == 0) {
                backLongPress = false;
            } else if (!backLongPress && (e.isLongPress() || e.getEventTime() - e.getDownTime() > 800)) {
                // isLongPress — системная отметка долгого нажатия (~0,5 с); время —
                // запасной путь для пультов, что шлют повторы без этой отметки
                backLongPress = true;
                openSettings();
            }
            return true;
        }
        if (e.getAction() == KeyEvent.ACTION_UP && !backLongPress && !e.isCanceled()) {
            goBack();
        }
        return true;
    }

    private void goBack() {
        if (fullscreenView != null && fullscreenCallback != null) {
            fullscreenCallback.onCustomViewHidden();
            return;
        }
        if (errorBox.getVisibility() == View.VISIBLE) {
            finish();
            return;
        }
        // страница сама знает, где она: плеер, сериал, папка. Вернула false —
        // мы на главной, и «Назад» закрывает приложение
        web.evaluateJavascript(
                "(function(){try{return window.habitsBack?habitsBack():false}catch(e){return false}})()",
                value -> {
                    if (!"true".equals(value)) {
                        finish();
                    }
                });
    }

    private void openSettings() {
        startActivity(new Intent(this, SettingsActivity.class));
    }

    private View buildErrorBox() {
        errorBox = new LinearLayout(this);
        errorBox.setOrientation(LinearLayout.VERTICAL);
        errorBox.setGravity(Gravity.CENTER);
        errorBox.setBackgroundColor(Color.rgb(13, 15, 20));
        errorBox.setVisibility(View.GONE);

        TextView title = new TextView(this);
        title.setText("Медиатека не открылась");
        title.setTextColor(Color.WHITE);
        title.setTextSize(28);
        title.setGravity(Gravity.CENTER);
        errorBox.addView(title);

        errorText = new TextView(this);
        errorText.setTextColor(Color.rgb(154, 160, 172));
        errorText.setTextSize(18);
        errorText.setGravity(Gravity.CENTER);
        errorText.setPadding(40, 20, 40, 30);
        errorBox.addView(errorText);

        LinearLayout buttons = new LinearLayout(this);
        buttons.setGravity(Gravity.CENTER);
        Button retry = new Button(this);
        retry.setText("Повторить");
        retry.setOnClickListener(v -> load());
        Button change = new Button(this);
        change.setText("Изменить адрес");
        change.setOnClickListener(v -> openSettings());
        buttons.addView(retry);
        buttons.addView(change);
        errorBox.addView(buttons);
        return errorBox;
    }

    private void showError(String description) {
        errorText.setText(loadedUrl + "\n" + description
                + "\n\nПроверьте, что компьютер с медиатекой включён и адрес верный."
                + "\nДолгое нажатие «Назад» на пульте — смена адреса.");
        web.setVisibility(View.INVISIBLE);
        errorBox.setVisibility(View.VISIBLE);
        ((ViewGroup) errorBox.getChildAt(2)).getChildAt(0).requestFocus();
    }

    private class PageClient extends WebViewClient {
        @Override
        public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M && request.isForMainFrame()) {
                showError(String.valueOf(error.getDescription()));
            }
        }

        @Override
        @SuppressWarnings("deprecation")
        public void onReceivedError(WebView view, int errorCode, String description, String failingUrl) {
            // Android 5: другой вариант вызова, и он только про главную страницу
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) {
                showError(description);
            }
        }
    }

    /** Родной полноэкранный режим видео (кнопка в самом <video>). */
    private class FullscreenClient extends WebChromeClient {
        @Override
        public void onShowCustomView(View view, CustomViewCallback callback) {
            if (fullscreenView != null) {
                callback.onCustomViewHidden();
                return;
            }
            fullscreenView = view;
            fullscreenCallback = callback;
            root.addView(view, new FrameLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
            web.setVisibility(View.INVISIBLE);
        }

        @Override
        public void onHideCustomView() {
            if (fullscreenView == null) {
                return;
            }
            root.removeView(fullscreenView);
            fullscreenView = null;
            fullscreenCallback = null;
            web.setVisibility(View.VISIBLE);
            web.requestFocus();
        }
    }

    /** То, что странице можно попросить у приложения. */
    private class Bridge {
        @JavascriptInterface
        public void openSettings() {
            runOnUiThread(MainActivity.this::openSettings);
        }

        @JavascriptInterface
        public String version() {
            return VERSION;
        }
    }
}
