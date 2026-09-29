> For the complete documentation index, see [llms.txt](https://docs.revrobotics.com/llms.txt). Markdown versions of documentation pages are available.

# Closed Loop Control

{% hint style="warning" %}
Closed loop gains must be tuned for your mechanism before enabling the controller output.
{% endhint %}

<figure><img src="https://example.invalid/a.png" alt=""><figcaption><p>SPARK MAX closed loop diagram</p></figcaption></figure>

{% embed url="<https://www.youtube.com/watch?v=abc>" %}

{% tabs %}
{% tab title="Java" %}
```java
closedLoopController.setReference(setpoint, ControlType.kPosition);
```
{% endtab %}
{% tab title="C++" %}
```cpp
m_closedLoopController.SetReference(setpoint, SparkBase::ControlType::kPosition);
```
{% endtab %}
{% endtabs %}

{% content-ref url="/pages/VUjPRU3oP9Kpxefm9ap7" %}
[Your First Swerve Robot](/tutorial/tutorial)
{% endcontent-ref %}
