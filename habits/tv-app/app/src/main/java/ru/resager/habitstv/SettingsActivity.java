package ru.resager.habitstv;

import android.app.Activity;
import android.graphics.Color;
import android.os.Bundle;
import android.text.InputType;
import android.view.Gravity;
import android.view.inputmethod.EditorInfo;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * Адрес медиатеки. Роутер может выдать компьютеру другой адрес — тогда его
 * меняют здесь, не переустанавливая приложение.
 */
public class SettingsActivity extends Activity {
    private EditText url;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);
        box.setGravity(Gravity.CENTER_HORIZONTAL);
        box.setPadding(80, 60, 80, 40);
        box.setBackgroundColor(Color.rgb(13, 15, 20));

        TextView title = new TextView(this);
        title.setText("Адрес медиатеки");
        title.setTextColor(Color.WHITE);
        title.setTextSize(28);
        box.addView(title);

        TextView hint = new TextView(this);
        hint.setText("Страница плеера на компьютере с медиаагентом, например "
                + MainActivity.DEFAULT_URL);
        hint.setTextColor(Color.rgb(154, 160, 172));
        hint.setTextSize(16);
        hint.setPadding(0, 16, 0, 24);
        box.addView(hint);

        url = new EditText(this);
        url.setSingleLine(true);
        url.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        // без полноэкранного поля клавиатуры: в горизонтальном экране оно
        // закрывает заголовок и подсказку, и непонятно, что вводишь
        url.setImeOptions(EditorInfo.IME_ACTION_DONE
                | EditorInfo.IME_FLAG_NO_EXTRACT_UI | EditorInfo.IME_FLAG_NO_FULLSCREEN);
        url.setTextColor(Color.WHITE);
        url.setTextSize(22);
        url.setText(MainActivity.savedUrl(this));
        url.setSelectAllOnFocus(true);
        url.setOnEditorActionListener((v, actionId, event) -> {
            if (actionId == EditorInfo.IME_ACTION_DONE) {
                save();
                return true;
            }
            return false;
        });
        box.addView(url, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        LinearLayout buttons = new LinearLayout(this);
        buttons.setGravity(Gravity.CENTER);
        buttons.setPadding(0, 24, 0, 0);
        buttons.addView(button("Сохранить", this::save));
        buttons.addView(button("Как было по умолчанию", () -> url.setText(MainActivity.DEFAULT_URL)));
        buttons.addView(button("Отмена", this::finish));
        box.addView(buttons);

        setContentView(box);
        url.requestFocus();
    }

    private Button button(String text, Runnable action) {
        Button b = new Button(this);
        b.setText(text);
        b.setOnClickListener(v -> action.run());
        return b;
    }

    private void save() {
        String value = url.getText().toString().trim();
        if (value.isEmpty()) {
            value = MainActivity.DEFAULT_URL;
        }
        // адрес набирают пультом — «192.168.0.79/tv» без схемы тоже должен работать
        if (!value.startsWith("http://") && !value.startsWith("https://")) {
            value = "http://" + value;
        }
        getSharedPreferences(MainActivity.PREFS, MODE_PRIVATE).edit()
                .putString(MainActivity.KEY_URL, value).apply();
        finish();
    }
}
